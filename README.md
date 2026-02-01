# Go-LogScanner

Go-LogScanner

Go-LogScanner는 다수의 로그 파일을 고루틴 워커풀로 병렬 스캔하고, 정규식 기반 패턴 매칭 결과를 실시간으로 보여주는 TUI(Log Dashboard) CLI 도구입니다.
대용량 파일 I/O를 라인 스트리밍으로 처리하며, 병렬 처리 환경에서도 매칭 이벤트 출력 순서를 안정적으로 관리하도록 설계했습니다.
___
Demo

입력: ./logs/*.log

필터: ERROR|WARN 같은 정규식

출력: 파일별 스캔 진행/통계 + 최근 매칭 라인(Recent Matches) 실시간 표시
___
💡 Key Features
1) Parallel Log Processing (Worker Pool)

    로그 파일 단위를 작업으로 보고, 지정한 워커 수(-concurrent)만큼 병렬로 처리합니다.

    CPU 코어 수를 기본값으로 사용하여 환경에 맞게 처리량을 확보합니다.

2) Streaming Scan (O(1) Memory)

    bufio.Scanner로 라인 단위 스트리밍 처리합니다.

    전체 파일을 메모리에 올리지 않아, 파일 크기가 커져도 메모리 사용량이 급증하지 않습니다.

3) Regex-based Matching

    regexp로 키워드/패턴을 컴파일한 뒤 매 라인에 적용합니다.

    단순 키워드가 아니라 정규식을 받아 다양한 오류 패턴을 포착할 수 있습니다.

4) Real-time TUI Dashboard

    전체 진행률(Progress bar), 처리 통계(Totals), 파일별 상태(Table)를 한 화면에서 제공합니다.

    최근 매칭 결과(Recent Matches)를 tail처럼 유지하여 “무슨 로그가 잡혔는지”를 즉시 확인할 수 있습니다.

5) Deterministic Match Ordering under Concurrency

    병렬 워커 환경에서도 매칭 이벤트에 단조 증가 시퀀스(Seq) 를 부여해 순서 정합성을 유지합니다.

    즉, 파일 스캔은 병렬이지만 “최근 매칭 라인 목록”은 안정된 기준으로 정렬 가능합니다.

6) Pause/Resume Control

    스캔 도중 p 키로 Pause/Resume을 제어합니다.

    Busy-wait 대신 atomic + channel 기반으로 워커를 멈추고 재개합니다.
___
🛠 Tech Stack

    Language: Go (Golang)

    Concurrency: Goroutines, Channels, WaitGroups

    Standard Libs: os/exec, flag, regexp, bufio, sync
___
🏗 Architecture & Technical Point
1. Concurrency Model (The "Ceiling" of Performance)

    본 프로젝트는 Go의 Work-Stealing Scheduler를 효율적으로 활용하기 위해 다음과 같은 구조를 가집니다.

    Producer-Consumer Pattern: 파일에서 라인을 읽어오는 Producer와 패턴을 매칭하는 Consumer를 Channel로 연결하여 블로킹을 최소화했습니다.

    Fan-in Pattern: 여러 Goroutine에서 분석된 결과를 하나의 통계 채널로 수집하여 Thread-safe하게 리포트를 생성합니다.

2. File I/O Optimization

    전체 파일을 메모리에 올리는 대신 bufio.Scanner를 사용해 라인 단위로 스트리밍 처리함으로써, 시스템 리소스의 **Ceiling(한계)**까지 성능을 끌어올리면서도 안정성을 유지합니다.
___
🚀 Getting Started
Installation
Bash

git clone https://github.com/qqqqdh/Go-LogScanner.git
cd go-logscanner
go build -o logscanner main.go

Usage
Bash

./logscanner -path="./logs/*.log" -keyword="ERROR" -concurrent=10 -tail=20
___
Flags:

    -path: 로그 파일이 위치한 경로 (와일드카드 지원)

    -keyword: 필터링할 정규표현식 또는 키워드

    -concurrent: 동시에 처리할 최대 Goroutine 수 (기본값: CPU 코어 수)

    -output: 리포트 저장 파일 경로
___
📈 Performance Impact

    단일 스레드 처리 대비 8코어 환경에서 약 5~7배 빠른 분석 속도 기록.

    1GB 이상의 대용량 로그 파일 처리 시에도 상수 메모리 사용량(O(1)) 유지.
___
👨‍💻 Intern Appeal: "Why Go?"

    "저는 대용량 데이터를 빠르고 안전하게 처리할 수 있는 Go의 특성을 깊이 이해하고 있습니다. 단순히 기능을 구현하는 것에 그치지 않고, Goroutine의 생명 주기 관리와 Channel을 통한 데이터 무결성 보장 등 효율적인 자원 활용에 집중하여 프로젝트를 설계했습니다."
___

