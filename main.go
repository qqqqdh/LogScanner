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

type FileStat struct {
	File    string
	Lines   int64
	Matches int64
	Err     error
}

func main() {
	path := flag.String("path", "./logs/*.log", "로그 파일 경로 (와일드카드 지원)")
	keyword := flag.String("keyword", "ERROR", "검색할 키워드/정규식")
	concurrent := flag.Int("concurrent", runtime.NumCPU(), "동시에 처리할 워커 수")
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
	fmt.Printf("동시 워커 수: %d\n\n", *concurrent)

	jobs := make(chan string)
	results := make(chan FileStat)

	var wg sync.WaitGroup

	// 1) 워커 생성
	wg.Add(*concurrent)
	for i := 0; i < *concurrent; i++ {
		go func(workerID int) {
			defer wg.Done()
			for file := range jobs {
				results <- processFileOnce(file, re) // ✅ Step 5 핵심(한 번 읽기)
			}
		}(i)
	}

	// 2) 워커 종료 후 results 닫기
	go func() {
		wg.Wait()
		close(results)
	}()

	// 3) jobs에 파일 넣기
	go func() {
		for _, f := range files {
			jobs <- f
		}
		close(jobs)
	}()

	// 4) Fan-in: main에서 결과 모아 합산
	var totalLines int64
	var totalMatches int64
	var ok, fail int

	fmt.Println("파일별 통계:")
	for r := range results {
		if r.Err != nil {
			fail++
			fmt.Printf("- %s: ERROR (%v)\n", r.File, r.Err)
			continue
		}
		ok++
		fmt.Printf("- %s: lines=%d, matches=%d\n", r.File, r.Lines, r.Matches)
		totalLines += r.Lines
		totalMatches += r.Matches
	}

	fmt.Printf("\n파일: %d개 (성공 %d, 실패 %d)\n", len(files), ok, fail)
	fmt.Printf("총 라인 수: %d\n", totalLines)
	fmt.Printf("총 매칭 수: %d\n", totalMatches)
}

// ✅ Step 5 핵심: 파일을 한 번만 읽어서 lines + matches를 동시에 계산
func processFileOnce(path string, re *regexp.Regexp) FileStat {
	f, err := os.Open(path)
	if err != nil {
		return FileStat{File: path, Err: err}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// 긴 라인(기본 64KB 제한) 대비 버퍼 확장
	const maxCapacity = 1024 * 1024 * 8 // 8MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	var lines int64
	var matches int64

	for scanner.Scan() {
		lines++
		line := scanner.Text()
		if re.MatchString(line) {
			matches++
		}
	}

	if err := scanner.Err(); err != nil {
		return FileStat{File: path, Lines: lines, Matches: matches, Err: err}
	}

	return FileStat{File: path, Lines: lines, Matches: matches, Err: nil}
}
