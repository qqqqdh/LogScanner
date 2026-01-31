package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
)

type LineEvent struct {
	File string
	Line string
}

type MatchEvent struct {
	File    string
	Matched bool
}

type FileStat struct {
	Lines   int64
	Matches int64
}

func main() {
	path := flag.String("path", "./logs/*.log", "로그 파일 경로 (와일드카드 지원)")
	keyword := flag.String("keyword", "ERROR", "검색할 키워드/정규식")
	concurrent := flag.Int("concurrent", runtime.NumCPU(), "Consumer 워커 수")
	flag.Parse()

	files, err := filepath.Glob(*path)
	if err != nil {
		log.Fatalf("glob 실패: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("로그 파일을 찾지 못했습니다: %s", *path)
	}

	re, err := regexp.Compile(*keyword)
	if err != nil {
		log.Fatalf("정규식 오류: %v", err)
	}
	if *concurrent <= 0 {
		*concurrent = 1
	}

	fmt.Printf("키워드/정규식: %s\n", *keyword)
	fmt.Printf("Consumer 워커 수: %d\n\n", *concurrent)

	// Producer -> Consumers
	linesCh := make(chan LineEvent, 4096)
	// Consumers -> Aggregator
	resultsCh := make(chan MatchEvent, 4096)

	// 1) Producer: 파일들을 순회하며 라인을 linesCh로 밀어넣음
	var prodWg sync.WaitGroup
	prodWg.Add(1)
	go func() {
		defer prodWg.Done()
		produceLines(files, linesCh)
	}()

	// Producer가 끝나면 linesCh 닫기
	go func() {
		prodWg.Wait()
		close(linesCh)
	}()

	// 2) Consumers: linesCh에서 라인을 받아 매칭 후 resultsCh로 보냄
	var consWg sync.WaitGroup
	consWg.Add(*concurrent)
	for i := 0; i < *concurrent; i++ {
		go func(workerID int) {
			defer consWg.Done()
			for ev := range linesCh {
				matched := re.MatchString(ev.Line)
				resultsCh <- MatchEvent{File: ev.File, Matched: matched}
			}
		}(i)
	}

	// Consumers가 끝나면 resultsCh 닫기
	go func() {
		consWg.Wait()
		close(resultsCh)
	}()

	// 3) Aggregator(Fan-in): 결과를 한 곳에서 받아 통계 집계
	perFile := make(map[string]FileStat)
	var totalLines int64
	var totalMatches int64

	for r := range resultsCh {
		stat := perFile[r.File]
		stat.Lines++
		totalLines++

		if r.Matched {
			stat.Matches++
			totalMatches++
		}
		perFile[r.File] = stat
	}

	// 출력
	fmt.Println("파일별 통계:")
	for _, f := range files {
		stat := perFile[f]
		fmt.Printf("- %s: lines=%d, matches=%d\n", f, stat.Lines, stat.Matches)
	}
	fmt.Printf("\n파일: %d개\n", len(files))
	fmt.Printf("총 라인 수: %d\n", totalLines)
	fmt.Printf("총 매칭 수: %d\n", totalMatches)
}

// Producer: 파일들에서 라인을 읽어서 linesCh로 전달
func produceLines(files []string, linesCh chan<- LineEvent) {
	for _, path := range files {
		if err := readFileLines(path, linesCh); err != nil {
			// Step 6에서는 “파일 읽기 에러 처리”를 단순화.
			// (원하면 Step 6.5에서 errCh 추가해서 파일별 실패 통계까지 넣자.)
			fmt.Fprintf(os.Stderr, "파일 읽기 실패: %s (%v)\n", path, err)
		}
	}
}

func readFileLines(path string, linesCh chan<- LineEvent) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// 긴 라인 대비 버퍼 확장
	const maxCapacity = 1024 * 1024 * 8 // 8MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		linesCh <- LineEvent{File: path, Line: scanner.Text()}
	}
	return scanner.Err()
}
