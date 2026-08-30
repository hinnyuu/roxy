// Package matcher 规则匹配（M2，无 LLM）：系列解析（本地索引为主、在线兜底）
// → 集映射 → 置信度分解 → 审核分流。设计见 docs/ARCHITECTURE.md §6/§9、
// D-014（置信度）/D-039（vault）/D-013（字幕配对）。
// M3 在此尾部插入 LLM 决策 + 验证器阶段，规则路径不变。
package matcher

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/hinnyuu/roxy/internal/domain"
	"github.com/hinnyuu/roxy/internal/metadata"
	"github.com/hinnyuu/roxy/internal/parser"
)

// Matcher 规则流水线。Client 为 nil 时禁用在线兜底（测试/离线）。
type Matcher struct {
	db        *sql.DB
	parser    *parser.Parser
	index     *metadata.Index
	client    *metadata.Client
	cache     *metadata.Cache
	threshold float64
	multiVer  string // vault | tolerate
	firstConf bool   // 系列首确认（D-043）
}

func New(db *sql.DB, p *parser.Parser, idx *metadata.Index, client *metadata.Client, cache *metadata.Cache, threshold float64, multiVersion string, seriesFirstConfirm bool) *Matcher {
	return &Matcher{db: db, parser: p, index: idx, client: client, cache: cache,
		threshold: threshold, multiVer: multiVersion, firstConf: seriesFirstConfirm}
}

// Outcome 单文件处理结果（可观测性 / 测试断言）。
type Outcome struct {
	SourceFileID int64
	PlacementID  int64
	ReviewState  string
	Confidence   float64
	Reason       string
	SeriesID     int64
}

type evidence struct {
	API           string   `json:"api"`
	ID            int64    `json:"id"`
	MatchedFields []string `json:"matched_fields"`
	EpVerified    bool     `json:"ep_verified"`
}

// ProcessEvent 流水线入口（scanner.Handler 的实现，触发归一化 §3）。
func (m *Matcher) ProcessEvent(ctx context.Context, ev domain.SourceEvent) error {
	var (
		fileID int64
		status string
	)
	err := m.db.QueryRowContext(ctx,
		`SELECT id, status FROM source_files WHERE abs_path = ?`, ev.AbsPath).Scan(&fileID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // 已被并发移除
	}
	if err != nil {
		return err
	}
	if status != domain.SourceFileNew {
		return nil
	}
	if err := m.setStatus(ctx, fileID, domain.SourceFileNew, domain.SourceFileParsing); err != nil {
		return err
	}

	pr := m.parser.Parse(filepath.Base(ev.AbsPath))
	if pr == nil {
		return m.setStatus(ctx, fileID, domain.SourceFileParsing, domain.SourceFileIgnored)
	}
	blob, _ := json.Marshal(pr)
	kind := parser.Classify(filepath.Base(ev.AbsPath))
	if _, err := m.db.ExecContext(ctx,
		`UPDATE source_files SET parse_result = ?, kind = ?, updated_at = ? WHERE id = ?`,
		string(blob), kind, domain.Now(), fileID); err != nil {
		return err
	}
	if err := m.setStatus(ctx, fileID, domain.SourceFileParsing, domain.SourceFileParsed); err != nil {
		return err
	}

	res, err := m.matchFile(ctx, fileID, pr, kind)
	if err != nil {
		return err
	}
	if res == nil {
		return nil // 未匹配：保持 parsed，UI 从文件视图可见
	}
	return m.setStatus(ctx, fileID, domain.SourceFileParsed, domain.SourceFilePlaced)
}

// matchFile 规则匹配核心；返回 nil 表示无系列候选（文件留在 parsed）。
func (m *Matcher) matchFile(ctx context.Context, fileID int64, pr *domain.ParseResult, kind string) (*Outcome, error) {
	cand, titleScore, err := m.resolveSeries(ctx, pr)
	if err != nil {
		return nil, err
	}
	if cand == nil {
		return nil, nil
	}
	seriesID, err := m.ensureSeries(ctx, cand)
	if err != nil {
		return nil, err
	}

	slot, season, seasonOK, ep, epEnd, ev, evidenceScore, reason := m.mapEpisode(ctx, cand.subjectID, pr)
	if cand.online != nil {
		ev.API = "bangumi-online"
	}
	if kind == parser.KindSubtitle && slot != domain.SlotIgnored {
		slot = domain.SlotSub // 字幕槽位；季/集沿用自身映射结果，配对见 D-013
	}
	confidence := 0.5*titleScore + 0.3*evidenceScore + 0.2*pr.Confidence
	if confidence > 1 {
		confidence = 1
	}
	// D-042：同名多候选且无年份消歧 → 封顶强制人工
	if cand.ambiguous && confidence >= m.threshold {
		confidence = m.threshold - 0.01
	}

	reviewState := domain.PlacementPendingReview
	if confidence >= m.threshold && slot != domain.SlotIgnored {
		reviewState = domain.PlacementAutoApproved
	} else if reason == "" {
		if cand.ambiguous {
			reason = "同名多候选（疑似重制/同名作品），请人工确认系列"
		} else {
			reason = fmt.Sprintf("置信度 %.2f < %.2f", confidence, m.threshold)
		}
	}

	// D-043：系列首确认——该系列尚无确认态决策时，自动放行降级为人工；
	// 纯状态推导，存量已放行系列天然豁免。
	if reviewState == domain.PlacementAutoApproved && m.firstConf {
		var confirmed bool
		if err := m.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM placements
			   WHERE series_id = ? AND review_state IN ('approved','auto_approved'))`, seriesID,
		).Scan(&confirmed); err != nil {
			return nil, err
		}
		if !confirmed {
			reviewState = domain.PlacementPendingReview
			reason = "系列首次确认（该系列尚无已确认决策）"
		}
	}

	vault := false
	if (slot == domain.SlotEpisode || slot == domain.SlotSpecial) && m.multiVer == "vault" {
		vault, err = m.existingVersion(ctx, seriesID, slot, season, ep)
		if err != nil {
			return nil, err
		}
	}

	subtitleOf := int64(0)
	if kind == parser.KindSubtitle {
		sid, ambiguous, perr := m.pairSubtitle(ctx, seriesID, ep, pr)
		if perr != nil {
			return nil, perr
		}
		subtitleOf = sid
		if ambiguous {
			reviewState = domain.PlacementPendingReview
			reason = "字幕版本配对仅定位到集（挂到主版本，请人工确认）"
		}
		if sid == 0 && reviewState == domain.PlacementAutoApproved {
			reviewState = domain.PlacementPendingReview
			reason = "字幕未找到对应视频版本"
		}
	}

	evJSON, _ := json.Marshal(ev)
	placement := &domain.Placement{
		SourceFileID: fileID, SeriesID: seriesID, SlotType: slot,
		Season: season, SeasonValid: seasonOK,
		Episode: deref(ep), EpisodeValid: ep != nil,
		EpisodeTitle: pr.EpisodeTitle,
		VersionKey:   pr.VersionKey, VersionLabel: pr.ReleaseGroup,
		Vault: vault, SubtitleOfPlacementID: subtitleOf,
		Confidence: confidence, DecisionSource: domain.DecisionRule,
		Evidence: string(evJSON), ReviewState: reviewState,
	}
	if epEnd != nil {
		placement.EpisodeEnd = *epEnd
		placement.EpisodeEndValid = true
	}
	pid, err := m.insertPlacement(ctx, placement)
	if err != nil {
		return nil, err
	}
	if reviewState == domain.PlacementPendingReview {
		if err := m.insertReviewCase(ctx, pid, reason); err != nil {
			return nil, err
		}
	}
	return &Outcome{SourceFileID: fileID, PlacementID: pid, ReviewState: reviewState,
		Confidence: confidence, Reason: reason, SeriesID: seriesID}, nil
}

// ---- 系列解析（§6 步骤 1，规则档）----

type seriesCand struct {
	subjectID  int64
	name       string
	nameCn     string
	platform   string
	date       string
	titleScore float64
	ambiguous  bool // 同名多候选且无年份消歧（D-042）
	online     *metadata.Subject
}

// nameHit 系列解析的统一候选形状（本地精确行 / FTS 命中）。
type nameHit struct {
	id       int64
	name     string
	nameCn   string
	platform string
	date     string
}

// sameNameVerdict 应用 D-042：统计与候选归一化标题全等的同名条目；
// ≥2 且文件年份（若有）不能唯一化 → 首选 + 歧义标记。
func sameNameVerdict(hits []nameHit, norm string, year *int) (nameHit, bool) {
	var same []nameHit
	for _, h := range hits {
		if domain.NormalizeTitle(h.name) == norm || domain.NormalizeTitle(h.nameCn) == norm {
			same = append(same, h)
		}
	}
	if len(same) == 0 {
		return hits[0], false
	}
	if len(same) == 1 {
		return same[0], false
	}
	if year != nil {
		prefix := fmt.Sprintf("%d-", *year)
		var kept []nameHit
		for _, h := range same {
			if strings.HasPrefix(h.date, prefix) {
				kept = append(kept, h)
			}
		}
		if len(kept) == 1 {
			return kept[0], false
		}
	}
	return same[0], true
}

// resolveSeries 本地别名 → bgm 精确 → FTS/LIKE → 在线兜底。
func (m *Matcher) resolveSeries(ctx context.Context, pr *domain.ParseResult) (*seriesCand, float64, error) {
	for _, cand := range pr.TitleCandidates {
		norm := domain.NormalizeTitle(cand)
		if norm == "" {
			continue
		}
		// a. 已收敛系列（别名精确，Go 侧归一化比对——M2 系列量级小）
		aliasRows, aerr := m.db.QueryContext(ctx,
			`SELECT s.bgm_subject_id, s.title, s.title_original FROM series_aliases a
			 JOIN series s ON s.id = a.series_id WHERE s.status = 'active'`)
		if aerr != nil {
			return nil, 0, aerr
		}
		for aliasRows.Next() {
			var (
				bgmID           int64
				title, titleOri string
			)
			if err := aliasRows.Scan(&bgmID, &title, &titleOri); err != nil {
				aliasRows.Close()
				return nil, 0, err
			}
			if domain.NormalizeTitle(title) == norm || domain.NormalizeTitle(titleOri) == norm {
				aliasRows.Close()
				return &seriesCand{subjectID: bgmID, name: titleOri, nameCn: title, titleScore: 1.0}, 1.0, nil
			}
		}
		aliasRows.Close()
		// b. 本地索引精确（归一化后全等；同名多候选走 D-042）
		rows, berr := m.db.QueryContext(ctx,
			`SELECT id, name, IFNULL(name_cn, ''), IFNULL(platform, ''), IFNULL(date, '') FROM bgm_subjects
			 WHERE nsfw = 0 AND (REPLACE(REPLACE(REPLACE(LOWER(name), ' ', ''), '-', ''), ':', '') = ?
			      OR REPLACE(REPLACE(IFNULL(LOWER(name_cn), ''), ' ', ''), '：', '') = ?)
			 LIMIT 5`, norm, norm)
		if berr != nil {
			return nil, 0, berr
		}
		var exactHits []nameHit
		for rows.Next() {
			var h nameHit
			if err := rows.Scan(&h.id, &h.name, &h.nameCn, &h.platform, &h.date); err != nil {
				rows.Close()
				return nil, 0, err
			}
			exactHits = append(exactHits, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, 0, err
		}
		if len(exactHits) > 0 {
			best, ambiguous := sameNameVerdict(exactHits, norm, pr.Year)
			return &seriesCand{subjectID: best.id, name: best.name, nameCn: best.nameCn,
				platform: best.platform, date: best.date, titleScore: 1.0, ambiguous: ambiguous}, 1.0, nil
		}
		// c. FTS/LIKE 检索（命中经 Go 侧归一化复核：全等 1.0 / 包含 0.8 / 弱匹配 0.7）
		hits, err := m.index.Search(ctx, cand, 5)
		if err != nil {
			return nil, 0, err
		}
		if len(hits) > 0 {
			nh := make([]nameHit, 0, len(hits))
			for _, h := range hits {
				nh = append(nh, nameHit{h.ID, h.Name, h.NameCn, h.Platform, h.Date})
			}
			best, ambiguous := sameNameVerdict(nh, norm, pr.Year)
			hn1, hn2 := domain.NormalizeTitle(best.name), domain.NormalizeTitle(best.nameCn)
			score := 0.7
			switch {
			case norm == hn1 || norm == hn2:
				score = 1.0
			case hn1 != "" && (strings.Contains(hn1, norm) || strings.Contains(norm, hn1)),
				hn2 != "" && (strings.Contains(hn2, norm) || strings.Contains(norm, hn2)):
				score = 0.8
			}
			return &seriesCand{subjectID: best.id, name: best.name, nameCn: best.nameCn,
				platform: best.platform, date: best.date, titleScore: score, ambiguous: ambiguous}, score, nil
		}
	}
	// d. 在线兜底（本地索引缺失/过旧时；结果必须经 GetSubject 核验，D-019）
	if m.client != nil && len(pr.TitleCandidates) > 0 {
		q := pr.TitleCandidates[0]
		if hits, err := m.client.SearchSubjects(ctx, m.cache, q); err == nil && len(hits) > 0 {
			if s, err := m.client.GetSubject(ctx, m.cache, hits[0].ID); err == nil && s.Type == 2 {
				return &seriesCand{subjectID: s.ID, name: s.Name, nameCn: s.NameCn,
					platform: s.Platform, date: s.Date, titleScore: 0.8, online: s}, 0.8, nil
			}
		}
	}
	return nil, 0, nil
}

// ensureSeries 收敛点：Series 实体（D-007）。
func (m *Matcher) ensureSeries(ctx context.Context, cand *seriesCand) (int64, error) {
	var id int64
	err := m.db.QueryRowContext(ctx,
		`SELECT id FROM series WHERE bgm_subject_id = ?`, cand.subjectID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	title := cand.nameCn
	if title == "" {
		title = cand.name
	}
	seriesType, libraryKind := mapPlatform(cand.platform)
	year := 0
	if len(cand.date) >= 4 {
		fmt.Sscanf(cand.date[:4], "%d", &year)
	}
	now := domain.Now()
	res, err := m.db.ExecContext(ctx,
		`INSERT INTO series (bgm_subject_id, title, title_original, year, series_type, library_kind, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		cand.subjectID, title, cand.name, year, seriesType, libraryKind, now, now)
	if err != nil {
		return 0, err
	}
	id, _ = res.LastInsertId()
	for _, alias := range []string{cand.name, cand.nameCn} {
		if strings.TrimSpace(alias) == "" {
			continue
		}
		if _, err := m.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO series_aliases (series_id, alias, source) VALUES (?, ?, 'api')`,
			id, alias); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// mapPlatform 平台 → series_type / library_kind。dump 数字码为主（RESEARCH §2：
// 1=TV 2=OVA 3=剧场版 4=短片 5=WEB 2006=漫画动画），在线 API 返回字符串，两者兼容。
func mapPlatform(platform string) (string, string) {
	p := strings.ToLower(strings.TrimSpace(platform))
	switch p {
	case "1", "tv":
		return "tv", "tv"
	case "2", "ova", "oad":
		return "ova", "tv"
	case "3", "movie", "film":
		return "movie", "movie"
	case "4":
		return "special", "tv"
	case "5", "web", "ona":
		return "ona", "tv"
	}
	switch {
	case strings.Contains(p, "剧") || strings.Contains(p, "movie") || strings.Contains(p, "film"):
		return "movie", "movie"
	case strings.Contains(p, "ova") || strings.Contains(p, "oad"):
		return "ova", "tv"
	case strings.Contains(p, "web"):
		return "ona", "tv"
	case strings.Contains(p, "tv"):
		return "tv", "tv"
	default:
		return "special", "tv"
	}
}

// ---- 集映射（§6 步骤 2）----

func (m *Matcher) mapEpisode(ctx context.Context, subjectID int64, pr *domain.ParseResult) (
	slot string, season int, seasonOK bool, ep, epEnd *float64,
	ev evidence, evidenceScore float64, reason string) {

	ev = evidence{API: "bangumi-local", ID: subjectID, MatchedFields: []string{"title"}}

	if pr.EPTypeHint == domain.HintMovie && pr.Episode == nil {
		return domain.SlotMovie, 0, false, nil, nil, ev, 0.7, ""
	}
	if pr.Batch {
		return domain.SlotIgnored, 0, false, nil, nil, ev, 0, "合集包（跨多集区间），禁止自动定位单集"
	}
	if pr.Episode == nil {
		switch pr.EPTypeHint {
		case domain.HintOP, domain.HintED, domain.HintPV, domain.HintCM:
			return slotForHint(pr.EPTypeHint), 0, true, nil, nil, ev, 0.6, ""
		case domain.HintSpecial, domain.HintOVA:
			return domain.SlotSpecial, 0, true, nil, nil, ev, 0.6, ""
		}
		return domain.SlotIgnored, 0, false, nil, nil, ev, 0, "无集数信息"
	}

	ep = pr.Episode
	epEnd = pr.EpisodeEnd
	var eps []domain.BgmEpisode
	if subjectID > 0 {
		eps, _ = m.index.Episodes(ctx, subjectID)
	}
	for _, e := range eps {
		if math.Abs(e.Sort-*ep) < 0.001 {
			ev.MatchedFields = append(ev.MatchedFields, "ep_sort")
			ev.EpVerified = true
			switch e.EPType {
			case 0:
				return domain.SlotEpisode, 1, true, ep, epEnd, ev, 1.0, ""
			case 1:
				return domain.SlotSpecial, 0, true, ep, epEnd, ev, 1.0, ""
			case 2:
				return domain.SlotOP, 0, true, ep, epEnd, ev, 1.0, ""
			case 3:
				return domain.SlotED, 0, true, ep, epEnd, ev, 1.0, ""
			case 4:
				return domain.SlotPV, 0, true, ep, epEnd, ev, 1.0, ""
			default:
				return domain.SlotExtra, 0, true, ep, epEnd, ev, 0.9, ""
			}
		}
	}
	// 集号未在索引验证：按提示归位，低证据分（→ 人工队列）
	if pr.EPTypeHint != domain.HintTV && pr.EPTypeHint != domain.HintUnknown {
		return slotForHint(pr.EPTypeHint), 0, true, ep, epEnd, ev, 0.4, ""
	}
	return domain.SlotEpisode, 1, true, ep, epEnd, ev, 0.4, ""
}

func slotForHint(hint string) string {
	switch hint {
	case domain.HintOP:
		return domain.SlotOP
	case domain.HintED:
		return domain.SlotED
	case domain.HintPV:
		return domain.SlotPV
	case domain.HintCM:
		return domain.SlotCM
	case domain.HintOVA:
		return domain.SlotSpecial
	default:
		return domain.SlotSpecial
	}
}

// ---- vault / 字幕配对 ----

// existingVersion 同槽位已有其他版本（先到为主，D-039）。
func (m *Matcher) existingVersion(ctx context.Context, seriesID int64, slot string, season int, ep *float64) (bool, error) {
	q := `SELECT EXISTS(SELECT 1 FROM placements
		WHERE series_id = ? AND slot_type = ? AND season = ? AND review_state != 'rejected'
		`
	args := []any{seriesID, slot, season}
	if ep != nil {
		q += ` AND ABS(episode - ?) < 0.001`
		args = append(args, *ep)
	} else {
		q += ` AND episode IS NULL`
	}
	q += `)`
	var exists bool
	err := m.db.QueryRowContext(ctx, q, args...).Scan(&exists)
	return exists, err
}

// pairSubtitle D-013 降级链：0 精确 basename → 1 同 version_key+集 →
// 2 只定到集（挂主版本+审核提示）→ 3 失败（人工）。
func (m *Matcher) pairSubtitle(ctx context.Context, seriesID int64, ep *float64, pr *domain.ParseResult) (placementID int64, ambiguous bool, err error) {
	if pr.SubtitleBase != "" {
		rows, qerr := m.db.QueryContext(ctx,
			`SELECT p.id, f.abs_path FROM placements p JOIN source_files f ON f.id = p.source_file_id
			 WHERE p.series_id = ? AND p.slot_type IN ('episode','special','movie') AND f.kind = 'video'`, seriesID)
		if qerr != nil {
			return 0, false, qerr
		}
		defer rows.Close()
		for rows.Next() {
			var pid int64
			var path string
			if err := rows.Scan(&pid, &path); err != nil {
				return 0, false, err
			}
			base := filepath.Base(path)
			videoBase := strings.TrimSuffix(base, filepath.Ext(base))
			if videoBase == pr.SubtitleBase {
				return pid, false, nil
			}
		}
	}
	if ep == nil {
		return 0, false, nil
	}
	var pid int64
	var vault int
	q := `SELECT p.id, p.vault FROM placements p
		WHERE p.series_id = ? AND p.slot_type = 'episode' AND ABS(p.episode - ?) < 0.001 AND p.review_state != 'rejected'`
	args := []any{seriesID, *ep}
	if pr.VersionKey != "" {
		q += ` AND p.version_key = ?`
		args = append(args, pr.VersionKey)
		err = m.db.QueryRowContext(ctx, q, args...).Scan(&pid, &vault)
		if err == nil {
			return pid, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, err
		}
		q2 := strings.Replace(q, ` AND p.version_key = ?`, ``, 1)
		if err := m.db.QueryRowContext(ctx, q2+` ORDER BY p.vault LIMIT 1`, seriesID, *ep).Scan(&pid, &vault); err == nil {
			return pid, true, nil
		}
		return 0, false, nil
	}
	err = m.db.QueryRowContext(ctx, q+` ORDER BY p.vault LIMIT 1`, args...).Scan(&pid, &vault)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return pid, true, nil
}

// ---- 落库辅助 ----

func (m *Matcher) insertPlacement(ctx context.Context, p *domain.Placement) (int64, error) {
	now := domain.Now()
	season := any(nil)
	if p.SeasonValid {
		season = p.Season
	}
	ep := any(nil)
	if p.EpisodeValid {
		ep = p.Episode
	}
	epEnd := any(nil)
	if p.EpisodeEndValid {
		epEnd = p.EpisodeEnd
	}
	res, err := m.db.ExecContext(ctx,
		`INSERT INTO placements (source_file_id, series_id, slot_type, season, episode, episode_end,
			episode_title, version_key, version_label, vault, subtitle_of_placement_id,
			confidence, decision_source, evidence, review_state, manual_lock, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SourceFileID, p.SeriesID, p.SlotType, season, ep, epEnd, p.EpisodeTitle,
		p.VersionKey, p.VersionLabel, b2i(p.Vault), nullInt64(p.SubtitleOfPlacementID),
		p.Confidence, p.DecisionSource, p.Evidence, p.ReviewState, b2i(p.ManualLock), now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (m *Matcher) insertReviewCase(ctx context.Context, placementID int64, reason string) error {
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO review_cases (placement_id, reason, state, created_at) VALUES (?, ?, 'open', ?)`,
		placementID, reason, domain.Now())
	return err
}

func (m *Matcher) setStatus(ctx context.Context, fileID int64, from, to string) error {
	if err := domain.SourceFileTransitionOK(from, to); err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx,
		`UPDATE source_files SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		to, domain.Now(), fileID, from)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
