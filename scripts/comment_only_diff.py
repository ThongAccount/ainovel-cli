#!/usr/bin/env python3
"""Kiểm tra một diff chỉ đổi comment Go, không đổi mã.

Cách dùng:

    python3 scripts/comment_only_diff.py <base>..<head>
    python3 scripts/comment_only_diff.py <base>...<head>
    bash scripts/check-vi-port.sh --comment-only-diff origin/main..HEAD

Dạng hai dấu chấm so thẳng hai ref. Dạng ba dấu chấm lấy merge-base làm
gốc — dùng dạng này khi nhánh đang đứng sau main, nếu không sẽ thấy cả
"thay đổi" của chính main.

Nguyên lý: bỏ mọi comment ra khỏi cả hai phiên bản của file rồi so token của
phần mã còn lại. Danh sách token giống nhau ⇒ thay đổi chỉ nằm trong comment.

So token, không so chuỗi thô, vì cách đó giữ đúng ba trường hợp hay gặp:

  * bỏ hẳn comment cuối dòng (`x := 1 // note` → `x := 1`) — chuỗi thô khác
    nhau vì khoảng trắng, token thì không;
  * đổi comment nằm giữa hai token (`f(a /*x*/, b)` → `f(a, b)`) — tương tự;
  * đổi khoảng trắng bên trong chuỗi (`"a  b"` → `"a b"`) — đây là thay đổi
    thật, token giữ nguyên chuỗi nên vẫn bị báo.

Khi so token, số dòng KHÔNG thuộc về nội dung mã: một comment viết lại
thành hai dòng làm mọi token phía sau lệch số dòng, dù mã không đổi. Nên
phần so sánh chỉ dùng nội dung token; số dòng chỉ để báo lỗi.

Mọi file khác (JSON, Markdown…) cũng bị báo: một track chỉ dịch comment thì
không nên đụng vào chúng.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")


class Issue:
    """Một sai lệch so với quy tắc "chỉ đổi comment"."""

    def __init__(self, path: str, kind: str, detail: str) -> None:
        self.path = path
        self.kind = kind
        self.detail = detail

    def __str__(self) -> str:
        return f"{self.path}: {self.kind}: {self.detail}"


def code_tokens(text: str) -> list[tuple[str, int]]:
    """Tách phần mã (không comment) thành token kèm số dòng.

    Comment `//` và `/* */` bị bỏ qua; chuỗi, raw string và rune được giữ
    nguyên như một token nên khoảng trắng bên trong chúng không bị coi là
    khác biệt.
    """
    tokens: list[tuple[str, int]] = []
    i, n = 0, len(text)
    line = 1
    while i < n:
        ch = text[i]
        if ch == "\n":
            line += 1
            i += 1
            continue
        if ch.isspace():
            i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "/":
            end = text.find("\n", i)
            i = n if end == -1 else end
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "*":
            end = text.find("*/", i + 2)
            stop = n if end == -1 else end + 2
            line += text[i:stop].count("\n")
            i = stop
            continue
        if ch in "\"`":
            j = i + 1
            while j < n:
                if ch == '"' and text[j] == "\\":
                    j += 2
                    continue
                if ch == '"' and text[j] == '"':
                    j += 1
                    break
                if ch == '"' and text[j] == "\n":
                    break
                if ch == "`" and text[j] == "`":
                    j += 1
                    break
                j += 1
            tokens.append((text[i:j], line))
            line += text[i:j].count("\n")
            i = j
            continue
        if ch == "'":
            j = i + 1
            while j < n:
                if text[j] == "\\":
                    j += 2
                    continue
                if text[j] == "'":
                    j += 1
                    break
                if text[j] == "\n":
                    break
                j += 1
            tokens.append((text[i:j], line))
            i = j
            continue
        if ch.isalpha() or ch == "_":
            j = i
            while j < n and (text[j].isalnum() or text[j] == "_"):
                j += 1
            tokens.append((text[i:j], line))
            i = j
            continue
        if ch.isdigit():
            j = i
            while j < n and (text[j].isalnum() or text[j] in "._"):
                j += 1
            tokens.append((text[i:j], line))
            i = j
            continue
        tokens.append((ch, line))
        i += 1
    return tokens


def token_texts(tokens: list[tuple[str, int]]) -> list[str]:
    """Danh sách nội dung token, bỏ số dòng.

    Số dòng không thuộc về nội dung mã: một comment viết lại thành nhiều dòng
    làm mọi token phía sau lệch số dòng, dù mã thì không đổi gì. So sánh phải
    bỏ số dòng, nếu không thì mọi comment dài thêm một dòng đều bị báo nhầm.
    """
    return [text for text, _ in tokens]


def git(*args: str) -> str:
    proc = subprocess.run(
        ["git", *args], check=False, capture_output=True, text=True
    )
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.strip() or f"git {' '.join(args)} thất bại")
    return proc.stdout


def git_show(ref: str, path: str) -> str | None:
    """Nội dung file tại một ref, None nếu file không tồn tại ở đó."""
    proc = subprocess.run(
        ["git", "show", f"{ref}:{path}"], check=False, capture_output=True, text=True
    )
    if proc.returncode != 0:
        return None
    return proc.stdout


def parse_range(value: str) -> tuple[str, str, bool]:
    """Chia phạm vi diff thành hai ref.

    Dạng `base..head` so thẳng hai ref. Dạng `base...head` lấy merge-base
    làm gốc — cần dạng này khi nhánh đang đứng sau main: so với main đã đi
    trước sẽ thấy "thay đổi" của chính main, không phải của nhánh.
    """
    if "..." in value:
        base, _, head = value.partition("...")
        triple = True
    elif ".." in value:
        base, _, head = value.partition("..")
        triple = False
    else:
        raise ValueError(
            f"phạm vi diff phải có dạng base..head hoặc base...head, nhận: {value}"
        )
    if not base or not head:
        raise ValueError(
            f"phạm vi diff phải có dạng base..head hoặc base...head, nhận: {value}"
        )
    return base, head, triple


def resolve_base(base: str, head: str) -> str:
    """Gốc so sánh của dạng `base...head`: điểm chung gần nhất."""
    out = git("merge-base", base, head).strip()
    return out or base


def changed_files(base: str, head: str) -> list[str]:
    out = git("diff", "--name-only", f"{base}..{head}")
    return [line for line in out.splitlines() if line.strip()]


def check_range(base: str, head: str) -> list[Issue]:
    """Trả về các vi phạm quy tắc "chỉ đổi comment" trong khoảng base..head."""
    issues: list[Issue] = []
    for path in changed_files(base, head):
        if not path.endswith(".go"):
            issues.append(
                Issue(path, "file ngoài phạm vi comment", "file không phải mã Go đã bị đổi")
            )
            continue
        old = git_show(base, path) or ""
        new = git_show(head, path) or ""
        old_tokens = code_tokens(old)
        new_tokens = code_tokens(new)
        if token_texts(old_tokens) == token_texts(new_tokens):
            continue
        detail = first_difference(old_tokens, new_tokens)
        issues.append(Issue(path, "mã đã thay đổi", detail))
    return issues


def first_difference(old: list[tuple[str, int]], new: list[tuple[str, int]]) -> str:
    """Mô tả khác biệt token đầu tiên giữa hai danh sách."""
    for index, (left, right) in enumerate(zip(old, new)):
        if left[0] != right[0]:
            return (
                f"token đầu tiên khác ở dòng {right[1]}: "
                f"{left[0]!r} → {right[0]!r} (vị trí {index})"
            )
    if len(old) != len(new):
        shorter, longer = (old, new) if len(old) < len(new) else (new, old)
        extra = longer[len(shorter)][0]
        return f"mã dài hơn {len(longer) - len(shorter)} token, token thừa: {extra!r}"
    return "mã thay đổi"


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    if len(argv) != 1:
        print(
            "Cách dùng: python3 scripts/comment_only_diff.py <base>..<head>",
            file=sys.stderr,
        )
        return 2
    try:
        base, head, triple = parse_range(argv[0])
        if triple:
            base = resolve_base(base, head)
    except ValueError as err:
        print(str(err), file=sys.stderr)
        return 2
    except RuntimeError as err:
        print(str(err), file=sys.stderr)
        return 2

    try:
        issues = check_range(base, head)
    except RuntimeError as err:
        print(str(err), file=sys.stderr)
        return 2

    if not issues:
        print(f"Không có thay đổi nào trong {base}..{head} (hoặc thay đổi chỉ nằm trong comment).")
        return 0
    for issue in issues:
        print(str(issue))
    print("")
    print(f"Phát hiện {len(issues)} vi phạm: diff này KHÔNG chỉ đổi comment.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
