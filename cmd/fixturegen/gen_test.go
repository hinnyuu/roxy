package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCreatesAllSpecs(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root, false); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, s := range specs() {
		path := filepath.Join(root, filepath.FromSlash(s.rel))
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing %s: %v", s.rel, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("empty file %s", s.rel)
		}
	}
}

func TestGenerateIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root, false); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if err := Generate(root, false); err != nil {
		t.Fatalf("second generate: %v", err)
	}
}

func TestNFOsAreWellFormedXML(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root, false); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, s := range specs() {
		if s.kind != kindNFO {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s.rel)))
		if err != nil {
			t.Fatalf("read %s: %v", s.rel, err)
		}
		dec := xml.NewDecoder(strings.NewReader(string(data)))
		for {
			_, err := dec.Token()
			if err != nil {
				if err.Error() == "EOF" {
					break
				}
				t.Errorf("malformed XML in %s: %v", s.rel, err)
				break
			}
		}
	}
}

func TestJPEGMagicBytes(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root, false); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, s := range specs() {
		if s.kind != kindJPEG {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s.rel)))
		if err != nil {
			t.Fatalf("read %s: %v", s.rel, err)
		}
		if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
			t.Errorf("%s is not a valid JPEG", s.rel)
		}
	}
}

func TestKeyEdgeCasePathsPresent(t *testing.T) {
	paths := map[string]bool{}
	for _, s := range specs() {
		paths[s.rel] = true
	}
	required := []string{
		// Season 00 与 Specials 两种目录名对照
		showA + "/Season 00/S00E01 - 第0话 序章.mkv",
		showB + "/Specials/S00E01 - FINAL话.mkv",
		// 多集合一
		showA + "/Season 01/S01E01E02 - 第01-02话 合并集.mkv",
		// 同集多版本 + 版本配套字幕
		showA + "/Season 01/S01E03 - 第03话 [AlphaSub].mkv",
		showA + "/Season 01/S01E03 - 第03话 [BetaSub].mkv",
		showA + "/Season 01/S01E03 - 第03话 [GammaSub].mkv",
		showA + "/Season 01/S01E03 - 第03话 [AlphaSub].zh-CN.srt",
		// Extras 探针
		showA + "/Extras/NCOP1.mkv",
		showA + "/Test Show Alpha-trailer.mkv",
		// 特殊字符
		showC + "/Season 01/S01E01 - 第01话「初阵」.mkv",
		// 剧场版
		movie + "/Test Movie 剧场版 (2025).mkv",
		movie + "/Test Movie 剧场版 (2025).nfo",
	}
	for _, r := range required {
		if !paths[r] {
			t.Errorf("required edge-case path missing from specs: %s", r)
		}
	}
}

func TestProbeGenerate(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root, true); err != nil {
		t.Fatalf("generate probe: %v", err)
	}
	list := probeSpecs()
	if len(list) != 10 {
		t.Errorf("probe specs = %d files, want 10", len(list))
	}
	for _, s := range list {
		path := filepath.Join(root, filepath.FromSlash(s.rel))
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Errorf("probe file missing/empty %s: %v", s.rel, err)
		}
	}
	required := []string{
		probeShow + "/Season 01/S01E01 - 第01话 - AlphaSub.mkv",
		probeShow + "/Season 01/S01E01 - 第01话 - BetaSub.mkv",
		probeShow + "/Season 01/S01E02 - 第02话 - 1080p.mkv",
		probeShow + "/Season 01/S01E02 - 第02话 - 720p.mkv",
		probeShow + "/Season 01/S01E03 - 第03话 [AlphaSub].mkv",
		probeShow + "/Season 01/S01E03 - 第03话 [BetaSub].mkv",
	}
	paths := map[string]bool{}
	for _, s := range list {
		paths[s.rel] = true
	}
	for _, r := range required {
		if !paths[r] {
			t.Errorf("probe variant missing: %s", r)
		}
	}
}
