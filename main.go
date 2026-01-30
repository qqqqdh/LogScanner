package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
)

func main() {
	path := flag.String("path", "./logs/*.log", "로그 파일 경로 (와일드카드 지원)")
	keyword := flag.String("keyword", "ERROR", "검색할 키워드/정규식")
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

	var totalLines int64
	var totalMatch int64

	fmt.Printf("키워드/정규식: %s\n\n", *keyword)
	fmt.Println("파일별 통계:")

	for _, f := range files {
		lines, err := countLines(f)
		if err != nil {
			fmt.Printf("- %s: lines=ERROR (%v)\n", f, err)
			continue
		}

		matches, err := countMatches(f, re)
		if err != nil {
			fmt.Printf("- %s: matches=ERROR (%v)\n", f, err)
			continue
		}

		fmt.Printf("- %s: lines=%d, matches=%d\n", f, lines, matches)
		totalLines += lines
		totalMatch += matches
	}

	fmt.Printf("\n총 라인 수: %d\n", totalLines)
	fmt.Printf("총 매칭 수: %d\n", totalMatch)
}

func countMatches(path string, re *regexp.Regexp) (int64, error) {
	file, err := os.Open(path)

	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	const maxCapacity = 1024 * 1024 * 8
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	var matches int64

	for scanner.Scan() {
		line := scanner.Text()

		if re.MatchString(line) {
			matches++
		}
	}

	if err := scanner.Err(); err != nil {
		return matches, err
	}
	return matches, nil
}

func countLines(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	const maxCapacity = 1024 * 1024 * 8 // 8MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	var lines int64
	for scanner.Scan() {
		lines++
	}

	if err := scanner.Err(); err != nil {
		return lines, err
	}
	return lines, nil
}
