package analyzer

import (
	"bufio"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

// Start: 파일 단위 병렬 스캔(워커풀) +
// - 파일별 결과(FileUpdate)
// - 전체 totals(Totals) 주기 전송
// - 매칭 라인(MatchLine) 전송
func Start(files []string, re *regexp.Regexp, concurrent int) <-chan Event {
	out := make(chan Event, 256)

	go func() {
		defer close(out)

		var filesDone int64
		var linesTotal int64
		var matchesTotal int64

		// 초기 totals 1회
		out <- Totals{FilesTotal: len(files)}

		jobs := make(chan string)

		var wg sync.WaitGroup
		wg.Add(concurrent)

		for i := 0; i < concurrent; i++ {
			go func() {
				defer wg.Done()
				for path := range jobs {
					lines, matches, err := scanFileOnce(path, re, out)
					if err != nil {
						out <- FileUpdate{
							File:    path,
							Lines:   lines,
							Matches: matches,
							Status:  "FAIL",
							Err:     err,
						}
					} else {
						out <- FileUpdate{
							File:    path,
							Lines:   lines,
							Matches: matches,
							Status:  "DONE",
							Err:     nil,
						}
						atomic.AddInt64(&linesTotal, lines)
						atomic.AddInt64(&matchesTotal, matches)
					}
					atomic.AddInt64(&filesDone, 1)
				}
			}()
		}

		// job feed
		go func() {
			for _, f := range files {
				jobs <- f
			}
			close(jobs)
		}()

		// totals ticker
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		for {
			select {
			case <-ticker.C:
				out <- Totals{
					FilesTotal:   len(files),
					FilesDone:    int(atomic.LoadInt64(&filesDone)),
					LinesTotal:   atomic.LoadInt64(&linesTotal),
					MatchesTotal: atomic.LoadInt64(&matchesTotal),
				}

			case <-done:
				out <- Totals{
					FilesTotal:   len(files),
					FilesDone:    int(atomic.LoadInt64(&filesDone)),
					LinesTotal:   atomic.LoadInt64(&linesTotal),
					MatchesTotal: atomic.LoadInt64(&matchesTotal),
					Done:         true,
				}
				return
			}
		}
	}()

	return out
}

// 파일을 한 번만 읽고(lines+matches), 매칭 라인은 out으로 전송
func scanFileOnce(path string, re *regexp.Regexp, out chan<- Event) (int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// 긴 라인 대비
	const maxCapacity = 1024 * 1024 * 8 // 8MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	var lines int64
	var matches int64

	for scanner.Scan() {
		lines++
		txt := scanner.Text()
		if re.MatchString(txt) {
			matches++
			out <- MatchLine{File: path, Line: txt}
		}
	}
	if err := scanner.Err(); err != nil {
		return lines, matches, err
	}
	return lines, matches, nil
}
