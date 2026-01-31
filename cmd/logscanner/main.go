package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"logscanner/internal/analyzer"
	"logscanner/internal/tui"
)

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

	updates := analyzer.Start(files, re, *concurrent)

	cfg := tui.Config{
		PathPattern: *path,
		Keyword:     *keyword,
		Concurrent:  *concurrent,
		TailMax:     *tail,
	}

	if err := tui.Run(files, updates, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "TUI 실행 실패:", err)
		os.Exit(1)
	}
}
