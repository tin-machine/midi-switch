# nanoKONTROL2 HDMI switch plan

この文書の詳細な検証ログは Hugo draft へ移行済み。

- 移行先: `../hugo/content/interface/midi-controller-monitor-control.md`

## 現在の扱い

このファイルは移行元の所在を残すための薄い参照だけにする。
実機の display serial、I2C bus、connector 名、手元環境の event dump、作業中の個別 mapping はここには残さない。

## 残す判断

- USB MIDI controller の入力は ALSA sequencer event として扱う。
- monitor 制御は `ddcutil` に寄せ、daemon 側で DDC/CI protocol を直接実装しない。
- 最初は `aseqdump` と外部 command 呼び出しで十分に検証し、仕様が固まってから native MIDI library へ移行する。
- 実装の現在値はこの repository の code / README を確認する。
