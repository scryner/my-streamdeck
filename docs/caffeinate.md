# Caffeinate: 잠금 중 잠자기 방지 일시 중단

## 동작

사용자 의도(`RequestedEnabled`)와 자식 프로세스의 실제 실행 상태(`Running`)를
분리한다. 현재 잠금 상태를 확인한 뒤 `RequestedEnabled && LockKnown && !Locked`
일 때만 `/usr/bin/caffeinate -di -w <worker PID>`를 실행한다.

| 사용자 설정 | 세션 상태 | 실제 실행 | 버튼 표시 |
| --- | --- | --- | --- |
| OFF | 무관 | 중단 | OFF |
| ON | 잠금 해제 확인됨 | 실행 | ON (초록색) |
| ON | 잠김 | 중단 | ON / LOCK (주황색) |
| ON | 상태 조회 불가 | 중단 | ON / WAIT (주황색) |
| ON | 잠금 해제됐으나 실행 실패/자식 종료 | 재시도 대기 | ON / RETRY (주황색) |

프로세스 종료 자체가 실패하면 핸들을 유지하고 후속 조회에서 종료를 재시도한다.
이때는 `ON / ERROR` 또는 `OFF / ERROR`로 아직 실행 중임을 표시한다.
실행 중인 자식은 하나이며, 종료를 기다리는 동안 새 자식을 시작하지 않는다.
예상치 못한 종료는 즉시 회수하고, 다음 주기에서 잠금 상태를 다시 확인한 뒤 재시도한다.

짧게 누르기(기존 250ms 이하)는 사용자 설정을 토글한다. 잠김 상태에서 OFF로
바꾸면 이후 잠금 해제나 절전 복귀에서도 다시 시작하지 않는다. 기존 길게 누르기
게이지/동작은 유지하며, 먼저 OFF 의도를 저장하고 소유한 caffeinate를 종료한 뒤
기존 System Events 잠자기 명령을 보낸다. 의도 저장이나 프로세스 종료가 실패하면
명시적 잠자기 명령은 보내지 않고 오류를 반환한다. 잠자기 명령 자체가 실패해도
의도는 OFF로 유지한다.

시간 표시는 **현재 ON 요청 이후 실제 caffeinate 실행 구간의 누적 시간**이다.
잠김, 조회 실패, 자식 종료로 실행이 중단된 동안에는 멈추고, 재개 시 이어 간다.
OFF 및 길게 누르기에서는 0으로 초기화한다. 자식 종료 시각은 회수/관측 시점
기준이므로 알림 처리 지연만큼의 오차는 있을 수 있다.

메뉴바 앱은 기존처럼 절전 복귀 때 작업 프로세스를 교체한다. ON 의도와 누적
시간은 메뉴바 앱이 소유한 임시 디렉터리에 원자적으로 저장하여 교체된 작업
프로세스에 전달한다. 새 작업 프로세스는 잠금 상태를 확인하기 전에는 실행하지
않는다. 메뉴바 앱 자체를 종료하면 임시 상태를 삭제하며 새 앱 세션은 OFF로
시작한다. 숨겨진 `--spawn`/`clock` 단독 실행은 이 임시 저장소 없이 메모리에서
상태를 관리한다. 비정상 작업 프로세스 종료 시 누적 시간은 마지막 저장 시점까지
복구되며, 정상 교체 시에는 종료 정리가 누적 시간을 저장한다.

## macOS 감지와 수명 관리

- `CGSessionCopyCurrentDictionary()`로 호출자의 WindowServer 세션을 조회한다.
  `CGSSessionScreenIsLocked`가 참일 때 잠김으로 판별한다. 이 키는 비공개 값이다.
  키가 없는 정상적인 로그인·콘솔 세션은 잠금 해제로 해석한다. 세션이 없거나
  필수 값이 없거나 잘못된 타입이면 조회 실패로 처리하며 잠금 해제로 간주하지 않는다.
  다른 사용자의 콘솔로 전환된 경우도 잠자기 방지를 중단한다.
- `com.apple.screenIsLocked`/`com.apple.screenIsUnlocked` 알림을 구독한다.
  잠김 알림은 우선 중단하고, 잠금 해제 알림만으로 실행을 재개하지 않는다.
  `NSWorkspaceDidWakeNotification` 및 세션 전환에서도 현재 상태를 다시 조회한다.
  화면 꺼짐이나 화면 보호기 알림은 사용하지 않는다.
- 시작 시, ON 요청 시, 알림 수신 시 및 250ms 주기로 조회한다. 알림이 빠져도
  정상적으로 스케줄되는 상태에서 다음 주기에 복구한다. 알림은 합쳐서 처리하고
  실제 상태를 기준으로 수렴하므로 중복 알림이 프로세스를 중복 생성하지 않는다.
  조회·프로세스 시작 중 새 알림이 오면 이전 결과로 실행을 유지하지 않는다.
- 메뉴바 없는 작업 프로세스도 메인 CFRunLoop를 돌려 네이티브 알림을 전달받는다.
  `clock` 단독 실행처럼 메인 루프가 없는 경로도 주기적 조회로 동작한다.
- 자신이 만든 `exec.Cmd`의 프로세스 핸들만 종료하고 `Wait`는 정확히 한 번 수행한다.
  앱 종료/컨텍스트 취소 시 관찰자와 자식을 정리한다. `-w <worker PID>`는 작업
  프로세스가 비정상 종료되어도 caffeinate가 assertion을 해제하도록 한다.
- 잠금 처리에서는 assertion만 해제한다. `pmset`, 시스템 설정 변경, 다른 앱의
  assertion 해제, 디스플레이 깨우기/즉시 재우기 명령은 실행하지 않는다.

`CGSessionCopyCurrentDictionary()`의 세션 부재 의미는
[Apple 문서](https://developer.apple.com/documentation/coregraphics/cgsessioncopycurrentdictionary())를
따른다. 분산 잠금 알림 이름은
[Chromium의 macOS 구현](https://github.com/chromium/chromium/blob/main/ui/base/idle/idle_mac.mm)에서도
사용하지만, 잠금 키와 알림 이름에는 공개 API 안정성 보장이 없으므로 macOS
업데이트 후 아래 수동 확인이 필요하다. 화면 보호기 자체를 잠금으로 취급하지 않는다.

## 자동 검증

테스트는 가짜 잠금 모니터, 가짜 프로세스/전원 백엔드, 테스트 전용 임시 파일만
사용한다. 실제 caffeinate 실행, Mac 잠금, 화면 꺼짐, 잠자기, 서비스 교체는 하지 않는다.

```sh
go test ./... -count=1 -timeout=90s
go test -race ./internal/widgets -run '^TestCaffeinate' -count=20 -timeout=90s
go test -race ./... -parallel=1 -count=1 -timeout=90s
go vet ./...
go build -o /tmp/my-streamdeck-lock-check .
```

검증 범위: 잠금/해제, 잠금 중 OFF/길게 누르기, 이미 잠긴 상태에서 ON 복구,
누락·중복·빠른 알림, 조회 실패·잘못된 세션 값, 오래된 조회/실행 결과 무효화,
실제 실행 시간의 정지·재개·초기화, 작업 프로세스 교체 시 의도 복구,
예상치 못한 자식 종료, 종료 대기와 새 실행의 경쟁, 종료 실패 시 소유권 유지,
컨텍스트 취소 및 늦게 도착한 콜백을 포함한다.

전체 병렬 race 검사는 기존 `volume_touch_test.go`의
`TestVolumeSystemBackendSetVolumeDoesNotMuteWhenUnmuted`와
`TestVolumeSystemBackendSetVolumePreservesMutedState`가 전역 setter를 병렬로
교체하는 경쟁 때문에 실패한다. `-parallel=1`로 실행해도 기존
`play_touch_test.go`의 `TestPlayTouchWidgetDialRotateNavigatesWithDebounce` 및
`TestPlayTouchWidgetDialRotateDebouncesSlowBackendByInputTime`에서 비동기 가짜
플레이어의 카운터를 동기화 없이 읽는 경쟁이 검출된다. 이 테스트 파일들은 이번
변경에서 수정하지 않았다. caffeinate 범위의 반복 race 검사는 별도로 실행한다.

2026-10-29 검증 결과: 전체 일반 테스트, caffeinate 범위의 20회 반복 race 검사,
앱 패키지 race 검사, `go vet ./...`, macOS 빌드는 통과했다. 전체 race 검사는
위 기존 테스트의 경쟁 때문에 통과하지 못했다. 빌드 결과는 임시 경로에만 만들었고
실행하지 않았다. 실행 중인 서비스의 교체·설치나 실제 잠금·잠자기는 수행하지 않았다.

## 이후 수동 확인 절차 (이번 작업에서는 실행하지 않음)

1. 테스트용 빌드를 실행한 환경에서 작업 프로세스 PID, 그 자식 caffeinate PID,
   버튼 표시, `pmset -g assertions` 결과를 기록한다. PID 확인에는
   `pgrep -P <worker PID> caffeinate`와 `ps -p <PID> -o pid,ppid,args`를 사용할 수 있다.
   새 메뉴바 앱 세션은 OFF여야 한다.
2. 잠금 해제 상태에서 짧게 눌러 ON으로 바꾼다. 소유한 자식 하나와 display/idle
   system assertion을 확인하고 누적 시간이 증가하는지 본다.
3. 사용자 조작으로 Mac을 잠근다. 소유한 자식과 해당 assertion이 사라지고 버튼이
   `ON / LOCK`이며 시간이 멈추는지 확인한다. 잠금 직후 즉시 잠자기를 강제하지
   않아야 한다. 시스템의 기존 유휴 시간에 따라 화면과 본체가 잠들 수 있어야 한다.
   다른 앱의 assertion이 있으면 해당 앱이 여전히 잠자기를 막을 수 있다.
4. 잠금을 유지한 채 화면만 깨운다. my-streamdeck의 caffeinate가 다시 생기지
   않아야 한다. 인증해 잠금을 해제한 뒤에만 자식 하나가 재생성되고 시간이 이어져야 한다.
5. ON으로 잠근 뒤 Stream Deck에서 OFF로 바꾸고 잠금을 해제한다. OFF가 유지되고
   자식이 없어야 한다. 잠금 중 새로 ON을 요청하면 `ON / LOCK`으로 대기해야 한다.
6. ON으로 잠긴 상태에서 절전과 복귀를 수행한다. 기존 작업 프로세스 교체 후에도
   `ON / LOCK`을 유지하고, 인증하여 잠금 해제할 때만 재개하는지 확인한다.
7. 잠금 해제 상태 및 `ON / LOCK` 상태 각각에서 길게 눌러 잠자기를 요청한다.
   복귀 및 작업 프로세스 교체 후 OFF이고 시간이 초기화되어 있어야 한다.
8. 잠금을 요구하지 않는 화면 보호기만 실행하거나 화면만 끄는 조건에서는
   해당 상태만을 이유로 `ON / LOCK`이 되지 않는지 확인한다. ON의 display assertion이
   일반적인 자동 화면 꺼짐을 막는 것은 정상이다.
9. 빠르게 잠금/해제를 반복하고 소유한 자식이 한 개를 넘지 않는지 본다. 소유한
   caffeinate만 종료해 예상치 못한 종료를 재현하면, 잠금 해제가 확인된 경우에만
   재생성되어야 한다. 별도 앱의 assertion은 그대로 유지되어야 한다.
10. 앱을 종료했을 때 소유한 자식과 assertion이 남지 않는지 확인한다.

이 절차의 실제 네이티브 잠금 알림 전달, 물리적 잠자기/절전 복귀 결과는 자동
테스트만으로 확인되지 않는다.
