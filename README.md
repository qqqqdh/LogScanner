🔍  LogScanner

    병렬 처리 기술을 활용한 고성능 TUI 로그 분석 대시보드

LogScanner는 다수의 로그 파일을 고루틴 워커풀(Worker Pool)로 병렬 스캔하고, 정규식 기반의 패턴 매칭 결과를 실시간 TUI 화면으로 제공하는 CLI 도구입니다. 대용량 파일도 안정적으로 처리할 수 있는 스트리밍 설계가 특징입니다.

✨ 핵심 기능 (Key Features)

🚀 1. 고성능 병렬 스캔 (Worker Pool)

    효율적인 리소스 활용: 지정한 워커 수(-concurrent)만큼 고루틴을 생성하여 여러 로그 파일을 동시에 스캔합니다.

    성능 최적화: 8코어 환경 기준, 단일 스레드 대비 약 5~7배 빠른 분석 속도를 기록합니다.

🛠️ 2. 리소스 최적화 설계 (O(1) Memory)

    라인 스트리밍: bufio.Scanner를 사용하여 파일을 라인 단위로 읽어 들입니다.

    안정성: 전체 파일을 메모리에 올리지 않으므로, 수 GB 이상의 대용량 로그 처리 시에도 메모리 사용량이 일정하게 유지됩니다.

📊 3. 실시간 TUI 대시보드

    지속적 감시: 파일을 한 번 읽고 끝내는 것이 아니라, tail -f와 같이 파일 끝에 새롭게 추가되는 로그를 즉시 감지하여 화면에 반영합니다.

    Follow 모드: 새로운 로그가 발생할 때    자동으로 최신 페이지의 마지막 줄로 화면을 이동시켜 실시간 상태를 놓치지 않게 도와줍니다.

⚖️ 4. 정밀한 제어 및 무결성

    순서 정합성 보장: 병렬 처리 중에도 각 이벤트에 시퀀스(Seq)를 부여하여 매칭 결과의 순서를 안정적으로 관리합니다.

    실시간 제어: 분석 중 p 키를 눌러 스캔을 일시정지하거나 재개할 수 있습니다.

🛠 기술 스택 (Tech Stack)

    언어: Go (Golang)

    동시성 모델: Goroutines, Channels, WaitGroups, Atomic Operations

    표준 라이브러리: regexp (정규식), bufio (스트리밍 I/O), sync (동기화)

🚀 시작하기 (Quick Start)
설치 (Installation)
Bash

git clone https://github.com/qqqqdh/LogScanner.git

cd go-logscanner

go build -o logscanner ./cmd/logscanner/main.go

실행 명령어 (Usage Examples)

1. 기본 실행 (특정 키워드 검색)
Bash

    ./logscanner -path="./logs/*.log" -keyword="ERROR"
![alt text](image.png)
2. 고성능 모드 (병렬 워커 수 지정)
Bash

    ./logscanner -path="./logs/*.log" -keyword="WARN|CRITICAL" -concurrent=10
![alt text](image-1.png)
3. 대시보드 출력 개수 조절
Bash

    ./logscanner -path="./logs/*.log" -keyword="FATAL" -tail=50
    ![alt text](image-2.png)

명령어 옵션 (Flags)
옵션	설명	기본값

    -path	로그 파일 경로 (와일드카드 지원)	./logs/*.log

    -keyword	검색할 정규표현식 또는 키워드	ERROR

    -concurrent	동시에 처리할 최대 고루틴 수	CPU 코어 수

    -tail	대시보드에 유지할 최근 매칭 라인 수	20
실시간 조작키

    p   전체 스캔 및 분석 작업 일시정지 / 재개
    q  프로그램 종료
    Tab 포커스 이동 - 로그 박스(왼쪽) ↔ 파일 목록 박스(오른쪽) 포커스 전환
    ← / →   이전 페이지 / 다음 페이지로 이동
    ↑ / ↓	현재 페이지 내 로그 줄 선택 이동 / 테이블 행 이동
    F	Follow 모드 활성화 (새 로그 발생 시 자동으로 마지막 페이지 추적)

👨‍💻 개발 의도 (Intern Appeal)

    "저는 대용량 데이터를 빠르고 안전하게 처리할 수 있는 Go의 동시성 모델을 깊이 이해하고 있습니다. 단순히 기능을 구현하는 것을 넘어, Worker Pool 패턴을 통한 리소스 최적화와 Channel을 통한 데이터 무결성 보장에 집중했습니다. 