# nanoKONTROL2 で HDMI ディスプレイ入力を切り替える Linux ツール作業計画書

## 目的

KORG nanoKONTROL2 の物理ボタン入力を Ubuntu Linux 上で受け取り、`ddcutil` を使って HDMI ディスプレイの入力ソースを切り替えるツールを作成する。

想定構成:

```text
nanoKONTROL2
  ↓ USB MIDI
Ubuntu Desktop
  ↓ ALSA MIDI event
自作 Go daemon
  ↓ ddcutil setvcp 60
HDMI / DP 接続ディスプレイ
  ↓
Input Source HDMI-1 / HDMI-2 切替
```

`ddcutil` は Linux から DDC/CI 経由でモニターの設定を読み書きするツールで、輝度・色・入力ソースなどを扱える。

公式ドキュメント: https://www.ddcutil.com/

MIDI 側の初期調査には ALSA の `aconnect` と `aseqdump` を使う。`aseqdump` は ALSA sequencer port で受信したイベントをテキスト表示するツール。

Ubuntu manpage: https://manpages.ubuntu.com/manpages/focal/man1/aseqdump.1.html

nanoKONTROL2 は MIDI デバイスとして、ボタン・ノブ・スライダーの操作を MIDI message として送信する。

KORG MIDI Implementation Chart: https://www.korg.com/us/support/download/manual/0/159/1911/

---

## 現在までに分かっている実機調査結果

### Ubuntu Desktop での `ddcutil detect`

実行コマンド:

```bash
ddcutil detect
```

実測結果:

```text
Display 1
   I2C bus:  /dev/i2c-2
   DRM_connector:           card1-HDMI-A-1
   EDID synopsis:
      Mfg id:               PHL - Philips Consumer Electronics Company
      Model:                PHL 224E5
      Product code:         49350  (0xc0c6)
      Serial number:        UHB1431012005
      Binary serial number: 12005 (0x00002ee5)
      Manufacture year:     2014,  Week: 31
   VCP version:         2.2

Display 2
   I2C bus:  /dev/i2c-3
   DRM_connector:           card1-DP-1
   EDID synopsis:
      Mfg id:               PHL - Philips Consumer Electronics Company
      Model:                PHL 224E5
      Product code:         49350  (0xc0c6)
      Serial number:        UHB1522025224
      Binary serial number: 25224 (0x00006288)
      Manufacture year:     2015,  Week: 22
   VCP version:         2.2

Display 3
   I2C bus:  /dev/i2c-4
   DRM_connector:           card1-DP-2
   EDID synopsis:
      Mfg id:               PHL - Philips Consumer Electronics Company
      Model:                PHL 224E5
      Product code:         49350  (0xc0c6)
      Serial number:        UHB1431010664
      Binary serial number: 10664 (0x000029a8)
      Manufacture year:     2014,  Week: 31
   VCP version:         2.2
```

### Display 1 の現在入力

実行コマンド:

```bash
ddcutil --display 1 getvcp 60
```

実測結果:

```text
VCP code 0x60 (Input Source): HDMI-1 (sl=0x11)
```

これにより、少なくとも Display 1 では以下が確定。

```text
HDMI-1 = 0x11
```

### 入力切替の実測

Display 1 に対して以下を実行したところ、HDMI-2 へ切り替わった。

```bash
ddcutil --display 1 setvcp 60 0x12
```

ただし Display 1 の HDMI-2 側には入力信号がないため、モニター側の自動入力検出により HDMI-1 へ自動的に戻った。

Display 2 では2つの入力が接続されており、以下の両方が成功した。

```bash
ddcutil --display 2 setvcp 60 0x11
ddcutil --display 2 setvcp 60 0x12
```

成功時にも以下のようなログが出る場合がある。

```text
(ddc_write_only_with_retry     ) Starting
(ddc_write_only_with_retry     ) Start of try loop, tryctr=0, max_tries=4, rc=-999, retryable=1
(ddc_write_only_with_retry     ) Done      Returning: NULL.
```

このログが出ても、実際に画面入力が切り替わっていれば成功として扱う。

`ddcutil setvcp` は通常は書き込み後に `getvcp` で検証するが、Input Source など一部の feature では検証が失敗しうるため、必要なら `--noverify` を検討する。

参考: https://www.ddcutil.com/command_setvcp/

---

## 方針

初期版は堅実さを優先し、Go から `aseqdump` と `ddcutil` を呼び出す。

```text
Go daemon
  ├─ aseqdump -p <MIDI port> を起動
  ├─ stdout から MIDI event を読む
  ├─ note_on / control_change を parse
  ├─ YAML 設定ファイルで action にマッピング
  └─ command 配列を exec.Command で実行
       └─ ddcutil --display 2 setvcp 60 0x11 / 0x12
```

初期版では `gomidi` を使わない。理由:

- `aseqdump` の出力は人間にも確認しやすい
- MIDI イベント番号の調査と実装を同じ見え方で進められる
- ALSA sequencer 経由で nanoKONTROL2 が見えていればすぐ動く
- 将来的に仕様が固まってから `gomidi` 直接実装へ移行すればよい

---

## Ubuntu 初期セットアップ

```bash
sudo apt update

sudo apt install -y \
  alsa-utils \
  ddcutil \
  i2c-tools \
  golang-go \
  git \
  make \
  pkg-config \
  libasound2-dev
```

用途:

```text
alsa-utils     : aconnect, aseqdump
ddcutil        : DDC/CI によるモニター制御
i2c-tools      : I2C/DDC 調査補助
golang-go      : 実装用
git/make       : 開発補助
pkg-config     : 将来 gomidi/rtmidi へ移行する場合に備える
libasound2-dev : ALSA 開発ヘッダ
```

一般ユーザーで `/dev/i2c-*` にアクセスできない場合は、必要に応じて以下を実行する。

```bash
sudo usermod -aG i2c "$USER"
```

反映にはログアウト/ログインが必要。

`i2c-dev` がロードされていない場合:

```bash
sudo modprobe i2c-dev
```

確認:

```bash
ls -l /dev/i2c-*
groups
```

---

## DDC / HDMI 側の調査手順

### 1. ディスプレイ検出

```bash
ddcutil detect
```

今回の実機では以下が有力な対象。

```text
Display 2
  I2C bus:       /dev/i2c-3
  DRM_connector: card1-DP-1
  Model:         PHL 224E5
  Serial:        UHB1522025224
  VCP version:   2.2
```

Display 2 は2つの入力が接続されており、`0x11` / `0x12` の切替・切り戻しが成功済み。

### 2. Input Source の capability 確認

```bash
ddcutil --display 2 capabilities | grep -A30 -i "Feature: 60"
```

広めに見る場合:

```bash
ddcutil --display 2 capabilities | grep -A40 -i "input"
```

### 3. 現在の入力確認

```bash
ddcutil --display 2 getvcp 60
```

### 4. 入力切替確認

```bash
ddcutil --display 2 setvcp 60 0x11
ddcutil --display 2 setvcp 60 0x12
```

必要に応じて `--noverify` を試す。

```bash
ddcutil --display 2 --noverify setvcp 60 0x11
ddcutil --display 2 --noverify setvcp 60 0x12
```

### 5. display 番号と bus 番号について

初期実装では検証済みの `--display 2` を使う。

ただし、`--display 2` は ddcutil の検出順に依存する可能性がある。  
将来的には以下のどれかに改善する。

```text
案A: --bus 3 を使う
案B: ddcutil detect を parse して serial number で対象を選ぶ
案C: config に display / bus / serial をすべて持たせ、起動時に検証する
```

今回の Display 2:

```yaml
target_display:
  ddcutil_display: 2
  i2c_bus: 3
  drm_connector: "card1-DP-1"
  manufacturer: "PHL"
  model: "PHL 224E5"
  serial: "UHB1522025224"
  vcp_version: "2.2"
```

---

## MIDI / nanoKONTROL2 側の調査手順

### 1. USB 認識確認

```bash
lsusb
```

nanoKONTROL2 らしき KORG デバイスが見えることを確認する。

### 2. ALSA sequencer port 確認

```bash
aconnect -l
```

期待例:

```text
client 24: 'nanoKONTROL2' [type=kernel,card=2]
    0 'nanoKONTROL2 MIDI 1'
```

この場合、MIDI port は `24:0`。

### 3. MIDI event 確認

```bash
aseqdump -p 24:0
```

ボタンを押してイベントを記録する。

例:

```text
Source  Event                  Ch  Data
 24:0   Note on                 0, note 41, velocity 127
 24:0   Note off                0, note 41, velocity 64
 24:0   Control change          0, controller 0, value 73
```

記録する項目:

```text
ボタン名:
  Event type: Note on / Note off / Control change
  Channel:
  Note番号 or Controller番号:
  Velocity or Value:
```

### 4. 推奨ボタン割り当て

初期案:

```text
REW   → Display 2 input 0x11
FF    → Display 2 input 0x12
STOP  → ddcutil getvcp 60
PLAY  → toggle 0x11 / 0x12
REC   → debug: 最後に受けたMIDIイベント表示
```

初期実装では `PLAY toggle` は後回しでもよい。  
まずは明示的に `0x11` / `0x12` に切り替える2ボタンを実装する。

---

## 実装要件

### 必須要件

- Go で実装する
- Ubuntu Linux 前提
- 初期版は `aseqdump` の stdout を読む
- 設定ファイルは YAML
- `note_on` と `control_change` に対応する
- MIDI event と command を設定ファイルでマッピングできる
- command は配列形式で指定する
- `sh -c` は使わない
- cooldown / debounce を入れる
- command timeout を入れる
- 実行ログを標準出力に出す
- systemd user service の例を含める
- README に調査手順を書く

### 非目標

初期版では以下はやらない。

- gomidi による MIDI 直接購読
- GUI
- 複数ディスプレイの自動選択
- nanoKONTROL2 の LED 制御
- 複雑な long press / combo 操作
- config hot reload

---

## ディレクトリ構成

```text
nanokontrol2-hdmi-switch/
  go.mod
  main.go
  config.example.yaml
  README.md
  systemd/
    nanokontrol2-hdmi-switch.service
```

---

## config.example.yaml

初期検証では Display 2 を対象にする。

```yaml
midi:
  port: "24:0"

defaults:
  cooldown_ms: 700
  command_timeout_sec: 5

actions:
  - name: display2-input-1
    event:
      type: note_on
      note: 41
      velocity_min: 1
    command:
      - ddcutil
      - --display
      - "2"
      - setvcp
      - "60"
      - "0x11"

  - name: display2-input-2
    event:
      type: note_on
      note: 42
      velocity_min: 1
    command:
      - ddcutil
      - --display
      - "2"
      - setvcp
      - "60"
      - "0x12"

  - name: display2-show-input
    event:
      type: note_on
      note: 43
      velocity_min: 1
    command:
      - ddcutil
      - --display
      - "2"
      - getvcp
      - "60"
```

`note: 41`, `note: 42`, `note: 43` は仮値。  
実際には `aseqdump` の結果に合わせて変更する。

必要なら `--noverify` 版も検討する。

```yaml
command:
  - ddcutil
  - --display
  - "2"
  - --noverify
  - setvcp
  - "60"
  - "0x12"
```

---

## main.go サンプル

```go
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MIDI struct {
		Port string `yaml:"port"`
	} `yaml:"midi"`

	Defaults struct {
		CooldownMS        int `yaml:"cooldown_ms"`
		CommandTimeoutSec int `yaml:"command_timeout_sec"`
	} `yaml:"defaults"`

	Actions []Action `yaml:"actions"`
}

type Action struct {
	Name    string   `yaml:"name"`
	Event   Event    `yaml:"event"`
	Command []string `yaml:"command"`
}

type Event struct {
	Type        string `yaml:"type"`
	Note        int    `yaml:"note"`
	VelocityMin int    `yaml:"velocity_min"`

	Controller int `yaml:"controller"`
	ValueMin   int `yaml:"value_min"`
	ValueMax   int `yaml:"value_max"`
}

type MIDIEvent struct {
	Type       string
	Channel    int
	Note       int
	Velocity   int
	Controller int
	Value      int
}

var (
	noteOnRe = regexp.MustCompile(`Note on\s+(\d+), note (\d+), velocity (\d+)`)
	ccRe     = regexp.MustCompile(`Control change\s+(\d+), controller (\d+), value (\d+)`)
	lastRun  = map[string]time.Time{}
)

func main() {
	configPath := flag.String("config", "config.example.yaml", "config file path")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if cfg.MIDI.Port == "" {
		log.Fatal("midi.port is required")
	}

	if cfg.Defaults.CooldownMS <= 0 {
		cfg.Defaults.CooldownMS = 700
	}
	if cfg.Defaults.CommandTimeoutSec <= 0 {
		cfg.Defaults.CommandTimeoutSec = 5
	}

	log.Printf("starting nanokontrol2-hdmi-switch")
	log.Printf("midi port: %s", cfg.MIDI.Port)

	cmd := exec.Command("aseqdump", "-p", cfg.MIDI.Port)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("failed to get stdout pipe: %v", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to start aseqdump: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		ev, ok := parseMIDIEvent(line)
		if !ok {
			continue
		}

		log.Printf("midi event: %+v", ev)

		for _, action := range cfg.Actions {
			if matchAction(action, ev) {
				if err := runAction(cfg, action); err != nil {
					log.Printf("action failed: name=%s err=%v", action.Name, err)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("scanner error: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		log.Fatalf("aseqdump exited: %v", err)
	}
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func parseMIDIEvent(line string) (MIDIEvent, bool) {
	if m := noteOnRe.FindStringSubmatch(line); m != nil {
		ch, _ := strconv.Atoi(m[1])
		note, _ := strconv.Atoi(m[2])
		velocity, _ := strconv.Atoi(m[3])

		return MIDIEvent{
			Type:     "note_on",
			Channel:  ch,
			Note:     note,
			Velocity: velocity,
		}, true
	}

	if m := ccRe.FindStringSubmatch(line); m != nil {
		ch, _ := strconv.Atoi(m[1])
		controller, _ := strconv.Atoi(m[2])
		value, _ := strconv.Atoi(m[3])

		return MIDIEvent{
			Type:       "control_change",
			Channel:    ch,
			Controller: controller,
			Value:      value,
		}, true
	}

	return MIDIEvent{}, false
}

func matchAction(action Action, ev MIDIEvent) bool {
	if action.Event.Type != ev.Type {
		return false
	}

	switch ev.Type {
	case "note_on":
		if action.Event.Note != ev.Note {
			return false
		}
		if ev.Velocity < action.Event.VelocityMin {
			return false
		}
		return true

	case "control_change":
		if action.Event.Controller != ev.Controller {
			return false
		}
		if action.Event.ValueMin != 0 && ev.Value < action.Event.ValueMin {
			return false
		}
		if action.Event.ValueMax != 0 && ev.Value > action.Event.ValueMax {
			return false
		}
		return true

	default:
		return false
	}
}

func runAction(cfg *Config, action Action) error {
	if len(action.Command) == 0 {
		return errors.New("empty command")
	}

	now := time.Now()
	cooldown := time.Duration(cfg.Defaults.CooldownMS) * time.Millisecond

	if prev, ok := lastRun[action.Name]; ok && now.Sub(prev) < cooldown {
		log.Printf("skip action due to cooldown: %s", action.Name)
		return nil
	}
	lastRun[action.Name] = now

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.Defaults.CommandTimeoutSec)*time.Second,
	)
	defer cancel()

	log.Printf("run action: %s command=%v", action.Name, action.Command)

	cmd := exec.CommandContext(ctx, action.Command[0], action.Command[1:]...)
	out, err := cmd.CombinedOutput()

	if len(out) > 0 {
		fmt.Printf("%s\n", out)
	}

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command timeout: %s", action.Name)
	}

	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}
```

---

## Go module 作成

```bash
go mod init example.com/nanokontrol2-hdmi-switch
go get gopkg.in/yaml.v3
go mod tidy
```

実行:

```bash
go run . -config config.example.yaml
```

ビルド:

```bash
go build -o nanokontrol2-hdmi-switch .
```

インストール例:

```bash
sudo install -m 0755 nanokontrol2-hdmi-switch /usr/local/bin/nanokontrol2-hdmi-switch
sudo mkdir -p /etc/nanokontrol2-hdmi-switch
sudo cp config.example.yaml /etc/nanokontrol2-hdmi-switch/config.yaml
```

---

## systemd user service 例

`systemd/nanokontrol2-hdmi-switch.service`

```ini
[Unit]
Description=nanoKONTROL2 HDMI input switcher
After=sound.target graphical-session.target
Wants=sound.target

[Service]
Type=simple
ExecStart=/usr/local/bin/nanokontrol2-hdmi-switch -config /etc/nanokontrol2-hdmi-switch/config.yaml
Restart=on-failure
RestartSec=3

StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
```

配置:

```bash
mkdir -p ~/.config/systemd/user
cp systemd/nanokontrol2-hdmi-switch.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now nanokontrol2-hdmi-switch.service
journalctl --user -u nanokontrol2-hdmi-switch.service -f
```

ログインしていない状態でも動かしたい場合は linger を検討する。

```bash
loginctl enable-linger "$USER"
```

---

## 検証手順

### Phase 1: DDC 単体確認

```bash
ddcutil --display 2 getvcp 60
ddcutil --display 2 setvcp 60 0x11
ddcutil --display 2 setvcp 60 0x12
```

成功条件:

```text
- Display 2 の入力が 0x11 側へ切り替わる
- Display 2 の入力が 0x12 側へ切り替わる
- 切り替え後も戻せる
```

### Phase 2: MIDI 単体確認

```bash
aconnect -l
aseqdump -p <nanoKONTROL2のport>
```

成功条件:

```text
- nanoKONTROL2 が ALSA sequencer port として見える
- 対象ボタン押下時に Note on または Control change が出る
- HDMI-1/HDMI-2 用に使うボタンの note/controller 番号を特定できる
```

### Phase 3: Go ツール確認

```bash
go run . -config config.example.yaml
```

成功条件:

```text
- ボタン押下で MIDI event ログが出る
- 対応 action が発火する
- ddcutil command が実行される
- Display 2 の入力が切り替わる
```

### Phase 4: systemd user service 確認

```bash
systemctl --user start nanokontrol2-hdmi-switch.service
journalctl --user -u nanokontrol2-hdmi-switch.service -f
```

成功条件:

```text
- systemd user service として起動する
- nanoKONTROL2 操作で入力切替できる
- journal に実行ログが残る
```

---

## 実装上の注意

### `sh -c` は使わない

設定ファイルから任意 command を実行するため、shell injection を避ける。

良い例:

```yaml
command:
  - ddcutil
  - --display
  - "2"
  - setvcp
  - "60"
  - "0x12"
```

避ける例:

```yaml
command:
  - sh
  - -c
  - "ddcutil --display 2 setvcp 60 0x12"
```

### cooldown / debounce を必ず入れる

MIDI ボタンは押下・離上・連打で複数イベントが出る。  
初期値は 700ms 程度。

```yaml
defaults:
  cooldown_ms: 700
```

### Note off は初期版では無視する

`aseqdump` では `Note on` / `Note off` の両方が出る可能性がある。  
初期版では `note_on` だけを action 対象にする。

### Input Source の値は固定しすぎない

今回の PHL 224E5 では以下で動いている。

```text
0x11 = input 1
0x12 = input 2
```

ただし別モニターでは異なる可能性がある。  
必ず `ddcutil getvcp 60` / `capabilities` / 実切替で確認する。

### `ddcutil` の成功判定

`setvcp 60` は入力切替により表示や DDC の状態が変化するため、書き込み後の検証が失敗する場合がある。  
実際に切り替わっていれば成功とみなす。必要なら config 側で `--noverify` を使えるようにする。

---

## トラブルシュート

### `ddcutil detect` でディスプレイが出ない

確認:

```bash
ls -l /dev/i2c-*
sudo modprobe i2c-dev
groups
```

必要なら:

```bash
sudo usermod -aG i2c "$USER"
```

### `ddcutil getvcp 60` が失敗する

可能性:

```text
- モニター側の DDC/CI が無効
- OSD 設定で DDC/CI が Off
- HDMI/DP ケーブルや変換アダプタが DDC を通していない
- その入力では DDC/CI が使えない
- VCP 60 Input Source に非対応
```

### `setvcp 60` で切り替わるが戻る

入力先に有効な映像信号がない場合、モニター側の自動入力検出で戻ることがある。  
今回 Display 1 で確認済み。

### `setvcp 60` のログがエラーっぽく見える

以下のようなログが出ても、実際に切り替わっていれば成功扱いでよい。

```text
(ddc_write_only_with_retry     ) Starting
(ddc_write_only_with_retry     ) Start of try loop, tryctr=0, max_tries=4, rc=-999, retryable=1
(ddc_write_only_with_retry     ) Done      Returning: NULL.
```

### MIDI イベントが出ない

確認:

```bash
lsusb
aconnect -l
aseqdump -p <port>
```

別 port に出ている可能性がある。  
nanoKONTROL2 を抜き差しして `aconnect -l` の変化を見る。

---

## 将来拡張案

### 1. HDMI toggle

現在入力を `getvcp 60` で読み、`0x11` なら `0x12`、`0x12` なら `0x11` にする。

```text
PLAY → toggle
```

注意点:

- `getvcp 60` が切替直後に正しく更新されないモニターがある
- 入力信号がない場合、前の入力を返すことがある

参考: https://www.ddcutil.com/archived/monitor_notes/

### 2. bus / serial による対象モニター選択

初期版は `--display 2` でよい。  
将来は以下を検討する。

```yaml
display_selector:
  serial: "UHB1522025224"
  fallback_bus: 3
```

起動時に `ddcutil detect` を実行し、serial が一致する display を探す。

### 3. 輝度制御

nanoKONTROL2 の slider を使って brightness を制御する。

```text
Slider 1 → ddcutil --display 2 setvcp 10 <0-100>
```

一般に VCP `0x10` は brightness として使われることが多いが、必ず `capabilities` で確認する。

### 4. 音量制御

ノブやスライダーを PulseAudio/PipeWire の音量制御に使う。

```bash
pactl set-sink-volume @DEFAULT_SINK@ 50%
```

### 5. SRE 操作卓化

nanoKONTROL2 を HDMI 切替だけでなく、運用操作卓として使う。

```text
STOP → tmux send-keys C-c
REC  → 調査ログ記録開始
MUTE → alert mute script
SOLO → kubectl logs 対象 namespace 切替
Slider → journalctl / kubectl logs の時間範囲変更
Knob   → grep severity / tail 件数変更
```

### 6. gomidi 直接実装

仕様が固まったら `aseqdump` 依存をなくす。

候補:

```text
gitlab.com/gomidi/midi/v2
gitlab.com/gomidi/midi/v2/drivers/rtmididrv
```

pkg.go.dev: https://pkg.go.dev/gitlab.com/gomidi/midi/v2

---

## Codex への作業依頼プロンプト

```text
このリポジトリで、KORG nanoKONTROL2 の MIDI 入力を受けて、ddcutil により HDMI/DP ディスプレイの入力ソースを切り替える Go ツールを作ってください。

前提:
- Ubuntu Desktop
- ディスプレイは ddcutil で検出済み
- 初期対象は ddcutil --display 2
- Display 2 では以下が実機で成功済み
  - ddcutil --display 2 setvcp 60 0x11
  - ddcutil --display 2 setvcp 60 0x12
- nanoKONTROL2 の MIDI イベントは aseqdump で調査する

要件:
- Go で実装
- 初期版は aseqdump の stdout を読む方式
- YAML 設定ファイルを使う
- note_on / control_change を action にマッピングできること
- action は command 配列として実行すること
- sh -c は使わないこと
- cooldown/debounce を入れること
- command timeout を入れること
- ddcutil setvcp 60 で 0x11 / 0x12 を切り替えるサンプル設定を含めること
- systemd user service のサンプルを含めること
- README に DDC 調査手順、MIDI 調査手順、トラブルシュートを書くこと

作成ファイル:
- main.go
- go.mod
- config.example.yaml
- README.md
- systemd/nanokontrol2-hdmi-switch.service

まず README に調査手順を書き、その後に実装してください。
```

---

## 完了条件

```text
[done] Ubuntu で ddcutil detect が成功している
[done] Display 1/2/3 が DDC/CI で検出されている
[done] Display 1 で HDMI-1 = 0x11 を確認済み
[done] Display 1 で 0x12 への切替成功。ただし入力信号なしで自動復帰
[done] Display 2 で 0x11 / 0x12 の切替・切り戻し成功

[next] nanoKONTROL2 の ALSA MIDI port を確認する
[next] aseqdump で対象ボタンの note/controller 番号を確定する
[next] config.example.yaml の note 番号を実測値に置き換える
[next] Go ツールを実装する
[next] systemd user service として動作確認する
```

---

## 2026-05-09 現地確認メモ

カレントホストで、この計画書の実現可能性と障害ポイントを確認した。

### パッケージ状態

計画書にある実行系パッケージは導入済みだった。

```text
alsa-utils 1.2.15.2-1ubuntu1
ddcutil 2.2.5-1
i2c-tools 4.4-2build3
golang-go 2:1.26~1
git 1:2.53.0-1ubuntu1
make 4.4.1-3
pkg-config 2.5.1-4
usbutils 1:019-1
```

不足していた `libasound2-dev` は追加済み。

```text
libasound2-dev 1.2.15.3-1ubuntu1
pkg-config --modversion alsa => 1.2.15.3
go version => go1.26.0 linux/amd64
```

現行の `aseqdump` 起動方式では `libasound2-dev` は実行時必須ではないが、将来 gomidi/rtmidi/ALSA 直結に寄せる場合の開発ヘッダとして意味がある。

### DDC / HDMI 側

`ddcutil detect` は計画書の実測値と一致した。

```text
Display 1: /dev/i2c-2, card1-HDMI-A-1, PHL 224E5, serial UHB1431012005
Display 2: /dev/i2c-3, card1-DP-1,     PHL 224E5, serial UHB1522025224
Display 3: /dev/i2c-4, card1-DP-2,     PHL 224E5, serial UHB1431010664
```

Display 2 は `--display 2` と `--bus 3` の両方で現在値を取得できた。

```text
ddcutil --display 2 getvcp 60 => HDMI-2 (sl=0x12)
ddcutil --bus 3 getvcp 60     => HDMI-2 (sl=0x12)
```

Display 2 の capabilities でも VCP `0x60` は対応済み。

```text
Feature: 60 (Input Source)
  01: VGA-1
  11: HDMI-1
  12: HDMI-2
```

したがって、DDC/CI で Display 2 の入力を `0x11` / `0x12` に切り替える実現可能性は高い。実装では計画書通り `--display 2` でも動くが、検出順変化を避けるなら `--bus 3` または serial 検証付きの指定に寄せる方が堅い。

注意点として、`ddcutil detect` と `ddcutil getvcp` を並列に実行した際に `/dev/i2c-2` の flock diagnostic が出た。結果は取得できたが、daemon 実装では `ddcutil` 呼び出しを直列化し、cooldown を入れる方針でよい。

### 権限

`/dev/i2c-*` は `root:i2c` かつ一部 ACL 付きで存在している。

```text
/dev/i2c-2
/dev/i2c-3
/dev/i2c-4
```

現在ユーザーの `groups` には `i2c` が含まれていないが、通常ユーザーで `ddcutil --bus 3 getvcp 60` は成功した。このホストでは ACL または ddcutil 側の実行条件により当面動作している可能性がある。ただし systemd user service 化後に失敗する場合は、`i2c` group 追加と再ログインを第一候補にする。

### MIDI / nanoKONTROL2 側

現時点では nanoKONTROL2 は USB に見えていない。

`lsusb` では KORG/nanoKONTROL2 相当のデバイスは未検出。`aconnect -l` と `/proc/asound/seq/clients` でも見えているのは `System`、`Midi Through`、`PipeWire-System`、`PipeWire-RT-Event` のみ。

```text
client 14: 'Midi Through'
client 142: 'PipeWire-System'
client 143: 'PipeWire-RT-Event'
```

`/proc/asound/cards` でも USB MIDI デバイスは出ておらず、HDA Intel PCH と HDA NVidia のみ。

このため、未完了の最大ポイントは nanoKONTROL2 の接続・認識確認。接続後に次を再実行する。

```bash
lsusb
cat /proc/asound/cards
aconnect -l
aseqdump -p <nanoKONTROL2のport>
```

接続済みなのに見えない場合の候補:

```text
1. USB ケーブルが充電専用、またはハブ/ポート相性
2. snd-usb-audio がデバイスを掴んでいない
3. nanoKONTROL2 側のモード、または KORG Kontrol Editor での設定差分
4. PipeWire の UMP MIDI2 表示に吸収され、期待した ALSA legacy port 名で見えていない
5. udev/権限問題。ただし /dev/snd/seq は root:audio で、現在ユーザーは audio group に所属済み
```

この段階では `note: 41` などの仮値は確定できない。nanoKONTROL2 を接続して `aseqdump` の実測が必要。

### systemd user service

`systemctl --user is-system-running` は `running`。`loginctl show-user` では user manager は active。

```text
RuntimePath=/run/user/1000
State=active
Linger=no
```

ログイン中に動かす systemd user service としては成立する。ログインしていない状態でも常駐させたい場合だけ、計画書通り `loginctl enable-linger "$USER"` を検討する。

Hugo 側の関連メモとして、`content/containerd/boot.md` と `content/k3s/rootless.md` に systemd user service / linger / `journalctl --user` の既存メモがある。この計画書の systemd user service 方針と矛盾はない。

### 実装時に反映したい修正案

`config.example.yaml` の ddcutil 指定は、初期値として `--display 2` でもよいが、実測済みの bus に固定する案も有力。

```yaml
command:
  - ddcutil
  - --bus
  - "3"
  - --noverify
  - setvcp
  - "60"
  - "0x12"
```

`--noverify` は、入力切替直後にディスプレイの応答が不安定になる場合の保険として設定で選べるようにするのがよい。

Go daemon 側は、同時に複数 `ddcutil` を起動しないよう action 実行を 1 本に直列化する。MIDI イベントの debounce だけでなく、DDC コマンド全体の mutex を持たせると flock diagnostic を避けやすい。

README には以下のトラブルシュートを入れる。

```text
- ddcutil detect は通るが setvcp が不安定: --bus 指定、--noverify、cooldown 増加を試す
- systemd user service でだけ ddcutil が失敗: i2c group、ACL、login session、journal を確認
- nanoKONTROL2 が見えない: lsusb -> /proc/asound/cards -> aconnect -l の順で切り分け
- aseqdump の port 番号は起動ごとに変わる可能性があるため、将来は port 名検索を実装する
```

### 2026-05-09 nanoKONTROL2 接続後の追加確認

nanoKONTROL2 接続後、USB/ALSA ともに認識された。

```text
lsusb:
  0944:0117 KORG, Inc. nanoKONTROL2 MIDI Controller

/proc/asound/cards:
  2 [nanoKONTROL2]: USB-Audio - nanoKONTROL2
     KORG INC. nanoKONTROL2 at usb-0000:00:14.0-10.3.1, full speed

aconnect -l:
  client 24: 'nanoKONTROL2' [type=kernel,card=2]
      0 'nanoKONTROL2 _ CTRL'
```

`aseqdump -p 24:0` で次のイベントを確認した。

```text
Control change channel 0, controller 48, value 127
Control change channel 0, controller 48, value 0
Control change channel 0, controller 49, value 127
Control change channel 0, controller 49, value 0
Control change channel 0, controller 50, value 127
Control change channel 0, controller 50, value 0
```

したがって、初期割り当ては以下でよい。

```text
controller 48, value_min 1 => Display 2 HDMI-1 (0x11)
controller 49, value_min 1 => Display 2 HDMI-2 (0x12)
controller 50, value_min 1 => Display 2 getvcp 60
```

ログイン時常駐用に、Go 実装、設定、systemd user unit を追加し、以下に配置した。

```text
~/.local/bin/midi-switch
~/.config/midi-switch/config.yaml
~/.config/systemd/user/midi-switch.service
```

`systemctl --user enable --now midi-switch.service` 済み。

```text
Active: active (running)
ExecStart: /home/kaoru/.local/bin/midi-switch -config /home/kaoru/.config/midi-switch/config.yaml
child: aseqdump -p 24:0
```

`midi.port_name: nanoKONTROL2` から起動時に ALSA sequencer port を探す実装にしたため、`24:0` が変化しても `aconnect -l` に nanoKONTROL2 が見えていれば追従する。

ログイン時に起動すればよい要件なので、`loginctl enable-linger` は設定していない。

### 2026-05-09 ボタン割り当て調査: Display1 HDMI-1 候補

Display1 は現在 `ddcutil --display 1 getvcp 60` で `HDMI-1 (sl=0x11)`。capabilities でも VCP `0x60` の `11: HDMI-1` と `12: HDMI-2` に対応している。

ユーザーが「M」と表現したボタンを `aseqdump -p 24:0` で監視した結果:

```text
Control change channel 0, controller 50, value 127
Control change channel 0, controller 50, value 0
```

したがって、M ボタンを Display1 HDMI-1 に割り当てる場合の event 条件は以下。

```yaml
event:
  type: control_change
  controller: 50
  value_min: 1
```

Display1 HDMI-1 への command 候補:

```yaml
command:
  - ddcutil
  - --display
  - "1"
  - --noverify
  - setvcp
  - "60"
  - "0x11"
```

または bus 固定で:

```yaml
command:
  - ddcutil
  - --bus
  - "2"
  - --noverify
  - setvcp
  - "60"
  - "0x11"
```

現行 `config.example.yaml` では controller 50 を Display2 の `getvcp 60` に仮割り当てしているため、M ボタンを Display1 HDMI-1 に使うならこの action は置き換える。

### 2026-05-09 M ボタン列の Display 対応

ユーザーが Display1 M、Display2 M、Display3 M の順に押したときの `aseqdump -p 24:0` 結果。

```text
Display1 M:
  Control change channel 0, controller 50, value 127
  Control change channel 0, controller 50, value 0

Display2 M:
  Control change channel 0, controller 49, value 127
  Control change channel 0, controller 49, value 0

Display3 M:
  Control change channel 0, controller 48, value 127
  Control change channel 0, controller 48, value 0
```

割り当て表:

```text
Display1 M => controller 50
Display2 M => controller 49
Display3 M => controller 48
```

押下だけを action に使うため、いずれも `value_min: 1` を条件にする。

Display1 M を Display1 HDMI-1 に割り当てる場合:

```yaml
event:
  type: control_change
  controller: 50
  value_min: 1
command:
  - ddcutil
  - --bus
  - "2"
  - --noverify
  - setvcp
  - "60"
  - "0x11"
```

調査後、`midi-switch.service` は意図通り停止したまま。

### 2026-05-09 HDMI-2 ボタン列の Display 対応

ユーザーが Display1 HDMI-2、Display2 HDMI-2、Display3 HDMI-2 の順に押したときの `aseqdump -p 24:0` 結果。

```text
Display1 HDMI-2:
  Control change channel 0, controller 66, value 127
  Control change channel 0, controller 66, value 0

Display2 HDMI-2:
  Control change channel 0, controller 65, value 127
  Control change channel 0, controller 65, value 0

Display3 HDMI-2:
  Control change channel 0, controller 64, value 127
  Control change channel 0, controller 64, value 0
```

割り当て表:

```text
Display1 HDMI-2 => controller 66
Display2 HDMI-2 => controller 65
Display3 HDMI-2 => controller 64
```

押下だけを action に使うため、いずれも `value_min: 1` を条件にする。

Display1 HDMI-2 に割り当てる場合:

```yaml
event:
  type: control_change
  controller: 66
  value_min: 1
command:
  - ddcutil
  - --bus
  - "2"
  - --noverify
  - setvcp
  - "60"
  - "0x12"
```

### 2026-05-09 明るさ操作子の調査

明るさ用候補として 4 回操作した結果。

```text
1回目:
  Control change channel 0, controller 2, value 117 -> 0 方向

2回目:
  Control change channel 0, controller 1, value 43 -> 127 方向

3回目:
  Control change channel 0, controller 0, value 1 -> 127 方向

4回目:
  Control change channel 0, controller 2, value 1 -> 127 方向
```

ユーザー確認により、4 回目の `controller 2` を Display1 の明るさ用スライダーとして扱う。

```text
Display1 brightness slider => controller 2
MIDI value range           => 0..127
DDC brightness range       => 0..100
DDC VCP code               => 0x10
Display1 bus               => /dev/i2c-2, ddcutil --bus 2
```

実装時は MIDI value を DDC value に変換する。

```text
ddc_value = round(midi_value * 100 / 127)
```

Display1 の明るさ反映コマンド例:

```bash
ddcutil --bus 2 --noverify setvcp 10 <0-100>
```

スライダーは大量のイベントを出すため、ボタン action と同じ実装では `ddcutil` を叩きすぎる。明るさ・コントラスト用途では以下を入れる。

```text
- controller ごとの throttle: 150ms から 300ms 程度
- 同一 DDC 値の連続送信を抑止
- ddcutil 実行は既存通り 1 本に直列化
- スライダー操作終了時の最終値をなるべく落とさない
```

### 2026-05-09 コントラスト操作子の調査

コントラスト用ボリュームを Display1、Display2、Display3 の順に操作した。

```text
Display1 contrast volume:
  Control change channel 0, controller 18, value 1 -> 127

Display2 contrast volume:
  Control change channel 0, controller 17, value 1 -> 127

Display3 contrast volume:
  Control change channel 0, controller 16, value 1 -> 127
```

割り当て表:

```text
Display1 contrast => controller 18
Display2 contrast => controller 17
Display3 contrast => controller 16
```

MIDI value は `0..127`、DDC contrast は VCP `0x12` の `0..100`。明るさと同じく以下で変換する。

```text
ddc_value = round(midi_value * 100 / 127)
```

Display1 のコントラスト反映コマンド例:

```bash
ddcutil --bus 2 --noverify setvcp 12 <0-100>
```

反応は重くてよい方針なので、throttle は 300ms から 500ms 程度でもよい。モニター DDC/CI の応答を優先し、連続操作中にキューを溜め込まず、最新値だけを残して反映する実装が向いている。

### 2026-05-09 実装反映

Go 実装に `ddc` action を追加した。従来の `command` 配列 action は残しつつ、DDC 用には以下を YAML で書ける。

```yaml
ddc:
  bus: 2
  vcp: "10"
  scale_from_midi: true
  min: 0
  max: 100
  noverify: true
  throttle_ms: 500
```

固定値の入力切替は以下の形。

```yaml
ddc:
  bus: 2
  vcp: "60"
  value: "0x11"
  noverify: true
```

`scale_from_midi: true` の action は MIDI `0..127` を DDC `min..max` に丸めて変換し、throttle 付きで最新値だけを反映する。DDC コマンド自体は従来通り直列化する。

現時点の `config.example.yaml` / `~/.config/midi-switch/config.yaml` には以下を反映済み。

```text
Display1 HDMI-1 => controller 50, bus 2, VCP 60, value 0x11
Display2 HDMI-1 => controller 49, bus 3, VCP 60, value 0x11
Display3 HDMI-1 => controller 48, bus 4, VCP 60, value 0x11

Display1 HDMI-2 => controller 66, bus 2, VCP 60, value 0x12
Display2 HDMI-2 => controller 65, bus 3, VCP 60, value 0x12
Display3 HDMI-2 => controller 64, bus 4, VCP 60, value 0x12

Display1 brightness => controller 2,  bus 2, VCP 10, scale 0..100
Display2 brightness => controller 1,  bus 3, VCP 10, scale 0..100
Display3 brightness => controller 0,  bus 4, VCP 10, scale 0..100

Display1 contrast => controller 18, bus 2, VCP 12, scale 0..100
Display2 contrast => controller 17, bus 3, VCP 12, scale 0..100
Display3 contrast => controller 16, bus 4, VCP 12, scale 0..100
```

Display1/2/3 と controller 番号が降順になっているのは意図的。ディスプレイの物理配置と nanoKONTROL2 上の操作子位置を一致させるため、見た目の並びを優先して対応づけている。

Display1-3 の明るさ操作子まで設定済み。

### 2026-05-09 Ubuntu Desktop 音量操作の調査と反映

Ubuntu Desktop は PipeWire 上の PulseAudio 互換で動作している。

```text
pactl info:
  Server Name: PulseAudio (on PipeWire 1.6.2)
  Default Sink: alsa_output.pci-0000_00_1b.0.analog-stereo

pactl get-sink-volume @DEFAULT_SINK@:
  63%
```

`pactl set-sink-volume @DEFAULT_SINK@ 62%` と `63%` への戻しを実行し、音量変更可能であることを確認した。

音量用操作子を動かした結果。

```text
Ubuntu default sink volume:
  Control change channel 0, controller 7, value 1 -> 127
```

割り当て:

```text
Ubuntu default sink volume => controller 7
MIDI value                 => 0..127
PulseAudio/PipeWire volume => 0..100%
```

Go 実装には `volume` action を追加した。`pactl set-sink-volume` により既定 sink を変更する。

```yaml
- name: ubuntu-default-sink-volume
  event:
    type: control_change
    controller: 7
  volume:
    target: "@DEFAULT_SINK@"
    min: 0
    max: 100
    throttle_ms: 300
```

反応は DDC より軽いが、連続イベントをそのまま全部流さないよう throttle 付きで最新値だけを反映する。

### 2026-05-09 Display2 明るさ操作子の調査と反映

Display2 の明るさ用ボリュームを操作した結果。

```text
Display2 brightness:
  Control change channel 0, controller 1, value 1 -> 127
```

割り当て:

```text
Display2 brightness => controller 1
Display2 bus        => /dev/i2c-3, ddcutil --bus 3
DDC VCP             => 0x10
```

`config.example.yaml` と `~/.config/midi-switch/config.yaml` に反映する。

```yaml
- name: display2-brightness
  event:
    type: control_change
    controller: 1
  ddc:
    bus: 3
    vcp: "10"
    scale_from_midi: true
    min: 0
    max: 100
    noverify: true
    throttle_ms: 500
```

### 2026-05-09 Display3 明るさ操作子の調査と反映

Display3 の明るさ用スライダーを操作した結果。

```text
Display3 brightness:
  Control change channel 0, controller 0, value 1 -> 127
```

割り当て:

```text
Display3 brightness => controller 0
Display3 bus        => /dev/i2c-4, ddcutil --bus 4
DDC VCP             => 0x10
```

`config.example.yaml` と `~/.config/midi-switch/config.yaml` に反映する。

```yaml
- name: display3-brightness
  event:
    type: control_change
    controller: 0
  ddc:
    bus: 4
    vcp: "10"
    scale_from_midi: true
    min: 0
    max: 100
    noverify: true
    throttle_ms: 500
```

確認:

```text
go test ./... => ok
go build -o midi-switch . => ok
/home/kaoru/.local/bin/midi-switch -config /home/kaoru/.config/midi-switch/config.yaml => midi port 24:0 まで起動確認
```

`midi-switch.service` はユーザーの希望通り停止したまま。

---

## 最初に Codex にやってほしい実作業

1. この Markdown を `docs/nanokontrol2-hdmi-switch-plan.md` として保存する
2. `README.md` に調査手順を整理する
3. `config.example.yaml` を作成する
4. `main.go` を実装する
5. `go run . -config config.example.yaml` で起動できる状態にする
6. `aseqdump` の実測結果をもとに note 番号を差し替える
7. systemd user service を追加する
