# midi-switch

KORG nanoKONTROL2 の MIDI 入力を ALSA `aseqdump` で読み、`ddcutil` でディスプレイの入力ソース、明るさ、コントラストを操作する。

## 現在の実測値

DDC 対象:

```text
Display 1
  I2C bus: /dev/i2c-2
  Input Source:
    0x11 HDMI-1
    0x12 HDMI-2

Display 2
  I2C bus: /dev/i2c-3
  Input Source:
    0x11 HDMI-1
    0x12 HDMI-2

Display 3
  I2C bus: /dev/i2c-4
  Input Source:
    0x11 HDMI-1
    0x12 HDMI-2
```

MIDI 対象:

```text
USB: 0944:0117 KORG, Inc. nanoKONTROL2 MIDI Controller
ALSA sequencer: client 24:0 'nanoKONTROL2 _ CTRL'
```

実測済みイベント:

```text
Display1 HDMI-1: controller 50
Display2 HDMI-1: controller 49
Display3 HDMI-1: controller 48

Display1 HDMI-2: controller 66
Display2 HDMI-2: controller 65
Display3 HDMI-2: controller 64

Display1 brightness: controller 2
Display2 brightness: controller 1
Display3 brightness: controller 0

Display1 contrast: controller 18
Display2 contrast: controller 17
Display3 contrast: controller 16

Ubuntu default sink volume: controller 7
```

この割り当ては、ディスプレイの物理配置と nanoKONTROL2 上の操作子位置を合わせるために、Display1/2/3 が controller 降順になる形で対応づけている。

連続値は MIDI `0..127` を DDC `0..100` に変換する。反応は重くてよい前提で、`ddc_throttle_ms: 500` にしている。

Display1-3 の入力切替、明るさ、コントラストと Ubuntu Desktop の既定 sink 音量を現設定に入れている。

## 実行

```bash
go run . -config config.example.yaml
```

ビルド:

```bash
go build -o midi-switch .
```

## ログイン時に常駐

```bash
mkdir -p ~/.local/bin ~/.config/midi-switch ~/.config/systemd/user
install -m 0755 midi-switch ~/.local/bin/midi-switch
install -m 0644 config.example.yaml ~/.config/midi-switch/config.yaml
install -m 0644 systemd/midi-switch.service ~/.config/systemd/user/midi-switch.service
systemctl --user daemon-reload
systemctl --user enable --now midi-switch.service
journalctl --user -u midi-switch.service -f
```

ログイン時に起動すればよいので、`loginctl enable-linger` は不要。

## 調査コマンド

DDC:

```bash
ddcutil detect
ddcutil --bus 3 capabilities
ddcutil --bus 3 getvcp 60
```

MIDI:

```bash
lsusb
cat /proc/asound/cards
aconnect -l
aseqdump -p 24:0
```

`24:0` は起動や抜き差しで変わる可能性があるため、daemon は `midi.port_name: nanoKONTROL2` から起動時に port を探す。

## 障害ポイント

`ddcutil` は I2C bus を掴むため、並列実行すると flock diagnostic が出ることがある。この daemon では action 実行を直列化している。

systemd user service でだけ DDC が失敗する場合は、`journalctl --user -u midi-switch.service` と `/dev/i2c-*` の ACL/group を確認する。必要なら `i2c` group への追加と再ログインを行う。
# midi-switch
