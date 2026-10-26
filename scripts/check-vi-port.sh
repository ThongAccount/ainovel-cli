#!/usr/bin/env bash
#
# check-vi-port.sh — cổng chất lượng Việt hóa cho ainovel-cli.
#
# CÁCH DÙNG
#   bash scripts/check-vi-port.sh [tùy chọn]
#
#   Script phải chạy từ repo root (tự chuyển về root qua git rev-parse),
#   dùng python3 + git, không cần mạng. Nếu máy không có `python3` thì
#   fallback sang `python` (runner Windows của GitHub Actions).
#
# TÙY CHỌN
#   --track-e            phạm vi Track E — CLI/TUI, bootstrap, logger, notify,
#                        version, models, utils, cmd                       [BẬT]
#   --track-a            phạm vi Track A — internal/tools                 [TẮT]
#   --track-b            phạm vi Track B — agents, arbiter, llmcontract,
#                        userrules, chapterfacts, stylestat                [TẮT]
#   --track-c            phạm vi Track C — internal/host                   [TẮT]
#   --track-d            phạm vi Track D — store, revision, flow, diag,
#                        eval, domain, rules                              [TẮT]
#   --all                bật tất cả các cờ track ở trên
#   --none               tắt tất cả các cờ track (chỉ in báo cáo cờ)
#   --production         chỉ quét mã production, bỏ qua file _test.go      [TẮT]
#   --strict-comments    coi ký tự CJK trong comment Go cũng là vi phạm     [TẮT]
#   --baseline <file>    literal CJK đã biết được xem là "đã biết", chỉ
#                        literal MỚI mới làm cổng đỏ
#   --update-baseline <file>
#                        ghi lại baseline hiện tại (dùng để ratchet backlog)
#   --list-flags         in danh sách cờ + đường dẫn được bảo vệ rồi thoát
#   -h, --help           in phần trợ giúp này rồi thoát
#
# MÃ THOÁT
#   0  không có vi phạm trong phạm vi đang bật
#   1  có chuỗi literal CJK (hoặc comment CJK khi --strict-comments) còn sót
#
# GHI CHÚ
#   * Track F (docs, assets/README.md, assets/testdata, evals/cases) không có
#     mã Go nên không có cổng kiểm tra literal CJK — tài liệu còn trích dẫn
#     ví dụ tiếng Trung hợp lệ.
#   * Các package chưa có track nào phụ trách (internal/errs, internal/i18n,
#     internal/llmretry) chỉ được kiểm tra khi Chief gán track.
#   * Bộ quét nằm trong `scripts/vi_port_scan.py`: nó tự nhận diện các hình
#     thức ngữ zh hợp lệ (bảng song ngữ `xxxZH`, trường `VI:`/`ZH:`, regex bắt
#     mã Trung, nhánh so khớp song ngữ, nhãn Việt nhúng mẫu Trung, hằng dữ
#     liệu tiếng Trung) nên không cần bảo vệ thủ công từng file.
#
set -euo pipefail

usage() {
	cat <<'EOF'
CÁCH DÙNG
  bash scripts/check-vi-port.sh [tùy chọn]

TÙY CHỌN
  --track-e            Track E (CLI/TUI + bootstrap/logger/notify/version/
                      models/utils/cmd)                         [BẬT]
  --track-a            Track A (internal/tools)                 [TẮT]
  --track-b            Track B (agents/arbiter/llmcontract/userrules/
                      chapterfacts/stylestat)                   [TẮT]
  --track-c            Track C (internal/host)                  [TẮT]
  --track-d            Track D (store/revision/flow/diag/eval/
                      domain/rules)                             [TẮT]
  --all                bật tất cả cờ track
  --none               tắt tất cả cờ track
  --production         chỉ quét mã production (bỏ qua _test.go) [TẮT]
  --strict-comments    ký tự CJK trong comment Go cũng là vi phạm [TẮT]
  --baseline <file>    literal đã biết không tính là vi phạm mới
  --update-baseline <file>
                      ghi lại baseline hiện tại
  --list-flags         in danh sách cờ + đường dẫn được bảo vệ
  -h, --help           in phần trợ giúp

MÃ THOÁT
  0  không có vi phạm
  1  còn chuỗi literal CJK trong phạm vi đang bật
EOF
}

# ---------------------------------------------------------------- cấu hình --

# Danh sách allowlist: các thư mục được kiểm tra khi cờ tương ứng được bật.
SCOPES_TRACK_A="internal/tools"
SCOPES_TRACK_B="internal/agents internal/arbiter internal/llmcontract internal/userrules internal/chapterfacts internal/stylestat"
SCOPES_TRACK_C="internal/host"
SCOPES_TRACK_D="internal/store internal/revision internal/flow internal/diag internal/eval internal/domain internal/rules"
SCOPES_TRACK_E="internal/entry internal/bootstrap internal/logger internal/notify internal/version internal/models internal/utils cmd"

# Danh sách denylist: đường dẫn luôn được bỏ qua vì còn CJK có chủ đích.
# Thư mục kết thúc bằng "/" nghĩa là bỏ qua cả cây thư mục.
# Bộ quét mới tự nhận diện bảng ngữ zh, nên danh sách này chỉ giữ lại những
# trường hợp không nhận diện được bằng hình thức câu lệnh.
PATHS_DENYLIST="assets/prompts/zh/|assets/references/zh/|assets/styles/zh/|assets/voice_zh.md|internal/store/labels.go|internal/rules/snapshot.go|internal/rules/lint.go|internal/tools/premise_structure.go|internal/host/exp/texts.go|internal/entry/tui/panels_test.go|internal/bootstrap/configfile_test.go"

enable_a=0
enable_b=0
enable_c=0
enable_d=0
enable_e=1
strict=0
production=0
baseline=""
update_baseline=""

list_flags() {
	printf 'Cờ hiện tại:\n'
	printf '  --track-e  : %s  %s\n' "$([[ $enable_e -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_E"
	printf '  --track-a  : %s  %s\n' "$([[ $enable_a -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_A"
	printf '  --track-b  : %s  %s\n' "$([[ $enable_b -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_B"
	printf '  --track-c  : %s  %s\n' "$([[ $enable_c -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_C"
	printf '  --track-d  : %s  %s\n' "$([[ $enable_d -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_D"
	printf '  --production: %s\n' "$([[ $production -eq 1 ]] && echo BẬT || echo TẮT)"
	printf '  --strict-comments: %s\n' "$([[ $strict -eq 1 ]] && echo BẬT || echo TẮT)"
	printf '  --baseline : %s\n' "${baseline:-<không có>}"
	printf '\nĐường dẫn được bảo vệ (luôn bỏ qua):\n'
	local entry
	while IFS= read -r entry; do
		printf '  %s\n' "$entry"
	done < <(printf '%s\n' "${PATHS_DENYLIST//|/$'\n'}")
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--track-e) enable_e=1 ;;
	--track-a) enable_a=1 ;;
	--track-b) enable_b=1 ;;
	--track-c) enable_c=1 ;;
	--track-d) enable_d=1 ;;
	--all)
		enable_a=1
		enable_b=1
		enable_c=1
		enable_d=1
		enable_e=1
		;;
	--none)
		enable_a=0
		enable_b=0
		enable_c=0
		enable_d=0
		enable_e=0
		;;
	--production) production=1 ;;
	--strict-comments) strict=1 ;;
	--baseline)
		[[ $# -ge 2 ]] || { printf 'Thiếu đường dẫn file sau --baseline.\n' >&2; exit 2; }
		baseline="$2"
		shift
		;;
	--update-baseline)
		[[ $# -ge 2 ]] || { printf 'Thiếu đường dẫn file sau --update-baseline.\n' >&2; exit 2; }
		update_baseline="$2"
		shift
		;;
	--list-flags)
		list_flags
		exit 0
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'Tùy chọn không hợp lệ: %s\n\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
	shift
done

scopes=""
add_scopes() {
	if [[ -n "$1" ]]; then
		scopes="${scopes:+$scopes }$1"
	fi
}
if [[ $enable_a -eq 1 ]]; then add_scopes "$SCOPES_TRACK_A"; fi
if [[ $enable_b -eq 1 ]]; then add_scopes "$SCOPES_TRACK_B"; fi
if [[ $enable_c -eq 1 ]]; then add_scopes "$SCOPES_TRACK_C"; fi
if [[ $enable_d -eq 1 ]]; then add_scopes "$SCOPES_TRACK_D"; fi
if [[ $enable_e -eq 1 ]]; then add_scopes "$SCOPES_TRACK_E"; fi

if [[ -z "$scopes" ]]; then
	printf 'Không có phạm vi nào được bật — dùng --list-flags để xem danh sách cờ.\n'
	exit 0
fi

PYTHON_BIN=""
if command -v python3 >/dev/null 2>&1; then
	PYTHON_BIN="python3"
elif command -v python >/dev/null 2>&1; then
	PYTHON_BIN="python"
else
	printf 'Thiếu python3 — cần python3 để kiểm tra literal CJK.\n' >&2
	exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCANNER="$SCRIPT_DIR/vi_port_scan.py"
if [[ ! -f "$SCANNER" ]]; then
	printf 'Không tìm thấy bộ quét: %s\n' "$SCANNER" >&2
	exit 2
fi

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# `--scope` nhận danh sách thư mục; mảng bash được truyền thành các tham số riêng.
scan_args=(--scope $scopes --denylist "$PATHS_DENYLIST")
[[ $production -eq 1 ]] && scan_args+=(--production)
[[ $strict -eq 1 ]] && scan_args+=(--strict-comments)
[[ -n "$baseline" ]] && scan_args+=(--baseline "$baseline")

if [[ -n "$update_baseline" ]]; then
	"$PYTHON_BIN" "$SCANNER" "${scan_args[@]}" --update-baseline "$update_baseline"
	exit 0
fi

if "$PYTHON_BIN" "$SCANNER" "${scan_args[@]}"; then
	exit 0
else
	status=$?
	printf '\nKiểm tra Việt hóa thất bại: còn chuỗi literal CJK trong phạm vi đang bật.\n' >&2
	exit "$status"
fi
