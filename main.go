package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	path := flag.String("path", "./logs/*.log", "로그 파일 경로 (와일드카드 지원)")
	flag.Parse()

	files, err := filepath.Glob(*path)
	if err != nil {
		log.Fatalf("glob 실패: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("로그 파일을 찾지 못했습니다: %s", *path)
	}

	var totalLines int64

	fmt.Println("파일별 라인 수:")
	for _, f := range files {
		n, err := countLines(f)
		if err != nil {
			fmt.Printf("- %s: ERROR (%v)\n", f, err)
			continue
		}
		fmt.Printf("- %s: %d lines\n", f, n)
		totalLines += n
	}

	fmt.Printf("\n총 라인 수: %d\n", totalLines)
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
