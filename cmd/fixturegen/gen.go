package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
)

type fileKind int

const (
	kindMedia fileKind = iota
	kindSRT
	kindASS
	kindNFO
	kindJPEG
)

type fileSpec struct {
	rel  string
	kind fileKind
	body string
}

const (
	showA = "tv/Test Show Alpha (2024)"
	showB = "tv/Test Show Beta ~Special Edition~ (2023)"
	showC = "tv/测试番剧Ω＆Δ (2025)"
	movie = "movies/Test Movie 剧场版 (2025)"

	probeShow = "tv/Probe Show (2024)"
)

// probeSpecs 是 M1.5 探针（D-038）：同一部番剧三集，各两个文件，
// 测试 Jellyfin 剧集多版本合并的后缀形态变体。
func probeSpecs() []fileSpec {
	return []fileSpec{
		{probeShow + "/tvshow.nfo", kindNFO, tvshowNFO("Probe Show", "プローブショー", 2024, 999005, 99005)},

		// 变体 A：" - "分隔组名后缀
		{probeShow + "/Season 01/S01E01 - 第01话 - AlphaSub.mkv", kindMedia, ""},
		{probeShow + "/Season 01/S01E01 - 第01话 - BetaSub.mkv", kindMedia, ""},
		{probeShow + "/Season 01/S01E01 - 第01话.nfo", kindNFO, episodeNFO("第01话", 1, 1, 999005001)},

		// 变体 B：" - "分隔技术标签后缀
		{probeShow + "/Season 01/S01E02 - 第02话 - 1080p.mkv", kindMedia, ""},
		{probeShow + "/Season 01/S01E02 - 第02话 - 720p.mkv", kindMedia, ""},
		{probeShow + "/Season 01/S01E02 - 第02话.nfo", kindNFO, episodeNFO("第02话", 1, 2, 999005002)},

		// 对照：方括号组名后缀（M1 已证 Jellyfin 不合并）
		{probeShow + "/Season 01/S01E03 - 第03话 [AlphaSub].mkv", kindMedia, ""},
		{probeShow + "/Season 01/S01E03 - 第03话 [BetaSub].mkv", kindMedia, ""},
		{probeShow + "/Season 01/S01E03 - 第03话.nfo", kindNFO, episodeNFO("第03话", 1, 3, 999005003)},
	}
}

func specs() []fileSpec {
	return []fileSpec{
		// ---------------- Show A：主测试番剧 ----------------
		{showA + "/tvshow.nfo", kindNFO, tvshowNFO("Test Show Alpha", "テストショーアルファ", 2024, 999001, 99001)},
		{showA + "/poster.jpg", kindJPEG, ""},
		{showA + "/fanart.jpg", kindJPEG, ""},

		// Season 00 特别篇（第 0 集 / 12.5 总集篇）
		{showA + "/Season 00/S00E01 - 第0话 序章.mkv", kindMedia, ""},
		{showA + "/Season 00/S00E01 - 第0话 序章.nfo", kindNFO, episodeNFO("第0话 序章", 0, 1, 999001001)},
		{showA + "/Season 00/S00E01 - 第0话 序章.zh-CN.srt", kindSRT, ""},
		{showA + "/Season 00/S00E02 - 第12.5话 总集篇.mkv", kindMedia, ""},
		{showA + "/Season 00/S00E02 - 第12.5话 总集篇.nfo", kindNFO, episodeNFO("第12.5话 总集篇", 0, 2, 999001002)},

		// Season 01
		{showA + "/Season 01/S01E01E02 - 第01-02话 合并集.mkv", kindMedia, ""},
		{showA + "/Season 01/S01E01E02 - 第01-02话 合并集.nfo", kindNFO, episodeNFO("第01-02话 合并集", 1, 1, 999001003)},

		// 同集多版本（三版本全部带后缀；字幕只配 AlphaSub 版本）
		{showA + "/Season 01/S01E03 - 第03话 [AlphaSub].mkv", kindMedia, ""},
		{showA + "/Season 01/S01E03 - 第03话 [BetaSub].mkv", kindMedia, ""},
		{showA + "/Season 01/S01E03 - 第03话 [GammaSub].mkv", kindMedia, ""},
		{showA + "/Season 01/S01E03 - 第03话 [AlphaSub].zh-CN.srt", kindSRT, ""},
		{showA + "/Season 01/S01E03 - 第03话.nfo", kindNFO, episodeNFO("第03话", 1, 3, 999001004)},

		// 普通集 + 多语言字幕
		{showA + "/Season 01/S01E04 - 第04话.mkv", kindMedia, ""},
		{showA + "/Season 01/S01E04 - 第04话.nfo", kindNFO, episodeNFO("第04话", 1, 4, 999001005)},
		{showA + "/Season 01/S01E04 - 第04话.zh-CN.srt", kindSRT, ""},
		{showA + "/Season 01/S01E04 - 第04话.zh-TW.srt", kindSRT, ""},
		{showA + "/Season 01/S01E04 - 第04话.ja.ass", kindASS, ""},

		// Extras 探针：目录约定 / 无约定命名 / -trailer 后缀（剧集根目录）
		{showA + "/Extras/Behind the Scenes.mkv", kindMedia, ""},
		{showA + "/Extras/NCOP1.mkv", kindMedia, ""},
		{showA + "/Test Show Alpha-trailer.mkv", kindMedia, ""},

		// ---------------- Show B：Specials 目录名 + 波浪号文件夹 ----------------
		{showB + "/tvshow.nfo", kindNFO, tvshowNFO("Test Show Beta ~Special Edition~", "テストショーベータ", 2023, 999002, 99002)},
		{showB + "/poster.jpg", kindJPEG, ""},
		{showB + "/Specials/S00E01 - FINAL话.mkv", kindMedia, ""},
		{showB + "/Specials/S00E01 - FINAL话.nfo", kindNFO, episodeNFO("FINAL话", 0, 1, 999002001)},
		{showB + "/Season 01/S01E01 - 第01话.mkv", kindMedia, ""},
		{showB + "/Season 01/S01E01 - 第01话.nfo", kindNFO, episodeNFO("第01话", 1, 1, 999002002)},

		// ---------------- Show C：中文与特殊字符（Ω、全角＆、Δ、「」） ----------------
		{showC + "/tvshow.nfo", kindNFO, tvshowNFO("测试番剧Ω＆Δ", "テストガンマ", 2025, 999003, 99003)},
		{showC + "/Season 01/S01E01 - 第01话「初阵」.mkv", kindMedia, ""},
		{showC + "/Season 01/S01E01 - 第01话「初阵」.nfo", kindNFO, episodeNFO("第01话「初阵」", 1, 1, 999003001)},

		// ---------------- 剧场版（电影库条目） ----------------
		{movie + "/Test Movie 剧场版 (2025).mkv", kindMedia, ""},
		{movie + "/Test Movie 剧场版 (2025).nfo", kindNFO, movieNFO()},
		{movie + "/Test Movie 剧场版 (2025).zh-CN.srt", kindSRT, ""},
		{movie + "/poster.jpg", kindJPEG, ""},
		{movie + "/fanart.jpg", kindJPEG, ""},
	}
}

func tvshowNFO(title, original string, year, bgmID, tmdbID int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tvshow>
  <title>%s</title>
  <originaltitle>%s</originaltitle>
  <year>%d</year>
  <plot>roxy M1 兼容性测试夹具（禁用刮削器后应完全从本 NFO 读取）。</plot>
  <uniqueid type="bangumi" default="true">%d</uniqueid>
  <uniqueid type="tmdb">%d</uniqueid>
  <genre>测试</genre>
  <studio>roxy-fixturegen</studio>
</tvshow>
`, title, original, year, bgmID, tmdbID)
}

func episodeNFO(title string, season, episode, bgmID int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<episodedetails>
  <title>%s</title>
  <season>%d</season>
  <episode>%d</episode>
  <aired>2024-01-%02d</aired>
  <plot>roxy M1 兼容性测试夹具剧集。</plot>
  <uniqueid type="bangumi" default="true">%d</uniqueid>
</episodedetails>
`, title, season, episode, episode, bgmID)
}

func movieNFO() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>Test Movie 剧场版</title>
  <originaltitle>テストムービー劇場版</originaltitle>
  <year>2025</year>
  <plot>roxy M1 兼容性测试夹具电影。</plot>
  <uniqueid type="bangumi" default="true">999004</uniqueid>
  <uniqueid type="tmdb">99004</uniqueid>
  <uniqueid type="imdb">tt9990004</uniqueid>
</movie>
`
}

func srtBody() string {
	return "1\n00:00:01,000 --> 00:00:03,000\nroxy 兼容性测试字幕\n"
}

func assBody() string {
	return `[Script Info]
ScriptType: v4.00+
PlayResX: 1920
PlayResY: 1080

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,48,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Actor, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,roxy 兼容性测试字幕
`
}

func dummyMedia(rel string) []byte {
	var buf bytes.Buffer
	buf.WriteString("ROXY DUMMY MEDIA — 仅用于命名/扫描兼容性测试，不可播放\n")
	buf.WriteString("path: " + rel + "\n")
	for buf.Len() < 4096 {
		buf.WriteString("0123456789abcdef")
	}
	return buf.Bytes()
}

func dummyJPEG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 80, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Generate 在 root 下生成完整假库；重复执行幂等（覆盖写入）。
// probe 为 true 时只生成 M1.5 探针库。
func Generate(root string, probe bool) error {
	list := specs()
	if probe {
		list = probeSpecs()
	}
	jpg, err := dummyJPEG()
	if err != nil {
		return fmt.Errorf("generate jpeg: %w", err)
	}
	for _, s := range list {
		path := filepath.Join(root, filepath.FromSlash(s.rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", s.rel, err)
		}
		var data []byte
		switch s.kind {
		case kindMedia:
			data = dummyMedia(s.rel)
		case kindSRT:
			data = []byte(srtBody())
		case kindASS:
			data = []byte(assBody())
		case kindNFO:
			data = []byte(s.body)
		case kindJPEG:
			data = jpg
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", s.rel, err)
		}
	}
	return nil
}

func printTree(root string, list []fileSpec) error {
	seen := map[string]bool{}
	var dirs []string
	for _, s := range list {
		dir := filepath.Dir(s.rel)
		for dir != "." && dir != "/" && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
			dir = filepath.Dir(dir)
		}
	}
	fmt.Printf("%s/\n", strings.TrimSuffix(root, "/"))
	for _, s := range list {
		fmt.Printf("  %s\n", s.rel)
	}
	return nil
}
