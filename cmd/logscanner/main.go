package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"logscanner/internal/analyzer"
	"logscanner/internal/tui"
)

// Windows 포함해서 "같은 파일"을 확실히 하나로 만들기:
// - Clean + Abs 로 경로 정규화
// - Windows는 대소문자 무시 => strings.ToLower 로 키 통일
func dedupeFiles(files []string) ([]string, error) {
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))

	for _, f := range files {
		clean := filepath.Clean(f)

		abs, err := filepath.Abs(clean)
		if err != nil {
			return nil, err
		}

		key := abs
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}

		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, abs) // 표시도 abs로 통일(원하면 clean으로 바꿔도 됨)
	}

	sort.Strings(out)
	return out, nil
}

func main() {
	path := flag.String("path", "./logs/*.log", "로그 파일 경로 (와일드카드 지원)")
	keyword := flag.String("keyword", "ERROR", "검색할 키워드/정규식")
	concurrent := flag.Int("concurrent", runtime.NumCPU(), "동시에 처리할 워커 수")
	tail := flag.Int("tail", 20, "Recent Matches(tail)에 유지할 라인 수")
	flag.Parse()

	files, err := filepath.Glob(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "glob 실패:", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "로그 파일을 찾지 못했습니다:", *path)
		os.Exit(1)
	}

	// ✅ 여기 추가: 중복 제거 + 정렬
	files, err = dedupeFiles(files)
	if err != nil {
		fmt.Fprintln(os.Stderr, "파일 경로 정규화 실패:", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "로그 파일을 찾지 못했습니다(중복 제거 후):", *path)
		os.Exit(1)
	}

	re, err := regexp.Compile(*keyword)
	if err != nil {
		fmt.Fprintln(os.Stderr, "정규식 오류:", err)
		os.Exit(1)
	}

	if *concurrent <= 0 {
		*concurrent = 1
	}
	if *tail <= 0 {
		*tail = 20
	}

	updates, pauseFn := analyzer.Start(files, re, *concurrent)

	cfg := tui.Config{
		PathPattern: *path,
		Keyword:     *keyword,
		Concurrent:  *concurrent,
		TailMax:     *tail,
	}

	if err := tui.Run(files, updates, cfg, pauseFn); err != nil {
		fmt.Fprintln(os.Stderr, "TUI 실행 실패:", err)
		os.Exit(1)
	}
}
