package parser

import (
	"bufio"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hinnyuu/roxy/internal/domain"
)

var update = flag.Bool("update", false, "重新生成 golden 期望值")

type goldenCase struct {
	File string              `json:"file"`
	Want *domain.ParseResult `json:"want"`
}

func loadCases(t *testing.T) []goldenCase {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "parser", "cases.jsonl"))
	if err != nil {
		t.Fatalf("open cases: %v", err)
	}
	defer f.Close()
	var out []goldenCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var c goldenCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("bad case line %q: %v", line, err)
		}
		out = append(out, c)
	}
	return out
}

func TestGolden(t *testing.T) {
	p := New(nil)
	cases := loadCases(t)
	got := make([]goldenCase, 0, len(cases))
	for _, c := range cases {
		g := p.Parse(c.File)
		if *update {
			got = append(got, goldenCase{File: c.File, Want: g})
			continue
		}
		wantJSON, _ := json.Marshal(c.Want)
		gotJSON, _ := json.Marshal(g)
		if string(wantJSON) != string(gotJSON) {
			t.Errorf("parse(%q):\n got %s\nwant %s", c.File, gotJSON, wantJSON)
		}
	}
	if *update {
		path := filepath.Join("..", "..", "testdata", "parser", "cases.jsonl")
		var sb strings.Builder
		for _, c := range got {
			b, _ := json.Marshal(c)
			sb.Write(b)
			sb.WriteString("\n")
		}
		if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"show.mkv":        KindVideo,
		"show.mp4":        KindVideo,
		"show.zh-CN.srt":  KindSubtitle,
		"show.ass":        KindSubtitle,
		"tvshow.nfo":      KindNFO,
		"poster.jpg":      KindImage,
		"readme.txt":      KindOther,
		"show.part01.rar": KindOther,
	}
	for name, want := range cases {
		if got := Classify(name); got != want {
			t.Errorf("Classify(%q) = %s, want %s", name, got, want)
		}
	}
}

func TestParseNonMedia(t *testing.T) {
	p := New(nil)
	if pr := p.Parse("poster.jpg"); pr != nil {
		t.Errorf("non-media should return nil, got %+v", pr)
	}
	if pr := p.Parse("tvshow.nfo"); pr != nil {
		t.Errorf("nfo should return nil, got %+v", pr)
	}
}
