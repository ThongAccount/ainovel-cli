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
#   --strict-comments    coi ký tự CJK trong comment Go cũng là vi phạm     [TẮT]
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
  --strict-comments    ký tự CJK trong comment Go cũng là vi phạm [TẮT]
  --list-flags         in danh sách cờ + đường dẫn được bảo vệ
  -h, --help           in trợ giúp

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
PATHS_DENYLIST="assets/prompts/zh/|assets/references/zh/|assets/styles/zh/|assets/voice_zh.md|internal/store/labels.go|internal/rules/snapshot.go|internal/rules/lint.go|internal/tools/premise_structure.go|internal/host/exp/texts.go|internal/entry/tui/panels_test.go|internal/bootstrap/configfile_test.go"

enable_a=0
enable_b=0
enable_c=0
enable_d=0
enable_e=1
strict=0

list_flags() {
	printf 'Cờ hiện tại:\n'
	printf '  --track-e  : %s  %s\n' "$([[ $enable_e -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_E"
	printf '  --track-a  : %s  %s\n' "$([[ $enable_a -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_A"
	printf '  --track-b  : %s  %s\n' "$([[ $enable_b -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_B"
	printf '  --track-c  : %s  %s\n' "$([[ $enable_c -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_C"
	printf '  --track-d  : %s  %s\n' "$([[ $enable_d -eq 1 ]] && echo BẬT || echo TẮT)" "$SCOPES_TRACK_D"
	printf '  --strict-comments: %s\n' "$([[ $strict -eq 1 ]] && echo BẬT || echo TẮT)"
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
	--strict-comments) strict=1 ;;
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

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

read -r -d '' PY_CHECK <<'PYTHON' || true
"""Quét file Go trong phạm vi allowlist, báo chuỗi literal CJK còn sót."""
import re
import subprocess
import sys

CJK = re.compile(r'[一-鿿]')


def scan(text):
    """Trả về (literals, comments) dạng list (offset, nội dung)."""
    literals, comments = [], []
    i, n = 0, len(text)
    while i < n:
        ch = text[i]
        if ch == '/' and i + 1 < n and text[i + 1] == '/':
            end = text.find('\n', i)
            end = n if end == -1 else end
            comments.append((i, text[i:end]))
            i = end
        elif ch == '/' and i + 1 < n and text[i + 1] == '*':
            end = text.find('*/', i + 2)
            end = n if end == -1 else end + 2
            comments.append((i, text[i:end]))
            i = end
        elif ch == '`':
            end = text.find('`', i + 1)
            end = n if end == -1 else end + 1
            literals.append((i, text[i:end]))
            i = end
        elif ch == '"':
            j = i + 1
            while j < n:
                if text[j] == '\\':
                    j += 2
                    continue
                if text[j] == '"':
                    j += 1
                    break
                if text[j] == '\n':
                    break
                j += 1
            literals.append((i, text[i:j]))
            i = j
        elif ch == "'":
            j = i + 1
            while j < n:
                if text[j] == '\\':
                    j += 2
                    continue
                if text[j] == "'":
                    j += 1
                    break
                if text[j] == '\n':
                    break
                j += 1
            i = j
        else:
            i += 1
    return literals, comments


def protected(path, entries):
    for entry in entries:
        if entry.endswith('/'):
            if path.startswith(entry):
                return True
        elif path == entry:
            return True
    return False


def main():
    strict = sys.argv[1] == '1'
    denylist = [p for p in sys.argv[2].split('|') if p]
    scopes = sys.argv[3].split()

    tracked = subprocess.run(
        ['git', 'ls-files', '--'] + scopes,
        check=True, capture_output=True, text=True,
    ).stdout.split()

    files = []
    skipped = []
    for path in tracked:
        if not path.endswith('.go'):
            continue
        if protected(path, denylist):
            skipped.append(path)
            continue
        files.append(path)

    violations = 0
    for path in files:
        try:
            text = open(path, encoding='utf-8', errors='replace').read()
        except OSError:
            continue
        literals, comments = scan(text)
        for offset, body in literals:
            if not CJK.search(body):
                continue
            line = text.count('\n', 0, offset) + 1
            source = text.splitlines()[line - 1].strip() if text.splitlines() else ''
            print('%s:%d: chuỗi literal CJK còn sót: %s' % (path, line, body))
            print('    %s:%d: %s' % (path, line, source))
            violations += 1
        if strict:
            for offset, body in comments:
                if not CJK.search(body):
                    continue
                line = text.count('\n', 0, offset) + 1
                source = text.splitlines()[line - 1].strip() if text.splitlines() else ''
                print('%s:%d: ký tự CJK còn sót trong comment: %s' % (path, line, body))
                print('    %s:%d: %s' % (path, line, source))
                violations += 1

    print('')
    print('Đã quét %d file Go trong phạm vi đang bật, bỏ qua %d file được bảo vệ.'
          % (len(files), len(skipped)))
    if violations:
        print('Phát hiện %d vi phạm CJK.' % violations)
        return 1
    print('Không còn chuỗi literal CJK trong phạm vi đang bật.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
PYTHON

if "$PYTHON_BIN" -c "$PY_CHECK" "$strict" "$PATHS_DENYLIST" "$scopes"; then
	exit 0
else
	status=$?
	printf '\nKiểm tra Việt hóa thất bại: còn chuỗi literal CJK trong phạm vi đang bật.\n' >&2
	exit "$status"
fi
