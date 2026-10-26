#!/usr/bin/env python3
"""Bộ quét literal CJK cho cổng Việt hóa của ainovel-cli.

Mục tiêu: phân biệt *chính xác* hai loại chuỗi chứa CJK trong mã Go:

  * preserved  — bản ngữ zh có chủ đích, giữ nguyên theo thiết kế:
                 bảng văn bản song ngữ (`xxxZH`), trường `VI:`/`ZH:`, hằng
                 dữ liệu tiếng Trung, regex bắt mã Trung, nhánh so khớp
                 song ngữ, nhãn tiếng Việt nhúng mẫu tiếng Trung.
  * violation  — chuỗi sót thật: thông điệp log, thông báo, lỗi, nhãn,
                 mô tả... mà người dùng đọc được và chưa được dịch.

Khác với bản quét nhúng sẵn trong `check-vi-port.sh` (v1), bộ quét này
nhận diện được các hình thức bản ngữ zh hợp lệ thay vì phải duy trì
thủ công một danh sách đường dẫn được bảo vệ.

Cách dùng trực tiếp (thường gọi qua `scripts/check-vi-port.sh`):

    python3 scripts/vi_port_scan.py --scope internal/host --production

Mã thoát:
    0  không có vi phạm chưa được ghi nhận trong baseline
    1  có chuỗi literal CJK sót thật
    2  lỗi tham số / môi trường
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from collections import Counter

# Runner Windows: stdout mặc định dùng cp1252, không mã hoá được chữ Việt
# (Đ) lẫn CJK — ép UTF-8 để bước in báo cáo không làm hỏng cổng kiểm tra.
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

CJK = re.compile(r"[一-鿿㐀-䶿]")

# Dấu tiếng Việt — dùng để nhận ra chuỗi đã dịch (kể cả khi nhúng mẫu
# tiếng Trung) và nhánh so khớp song ngữ.
VIETNESE = re.compile(r"[ăâđêôơưĂÂĐÊÔƠƯạảấầẩẫậắằẳẵặẹẻẽếềểễệỉịọỏốồổỗộớờởỡợụủứừửữựỳỵỷỹ]")

# Định danh bảng ngữ zh: `workerTextsZH`, `toolTextsZH`, `coCreateSystemPromptZH`…
ZH_IDENT = re.compile(r"\b([A-Za-z_]\w*ZH\w*)\b")

# Lời gọi so khớp: chuỗi Trung dùng để *nhận diện* nội dung đầu vào.
MATCH_CALL = re.compile(
    r"\b(?:strings\.(?:Contains|ContainsAny|HasPrefix|HasSuffix|EqualFold)|"
    r"bytes\.(?:Contains|HasPrefix|HasSuffix))\s*\("
)

REGEXP_CALL = re.compile(r"\bregexp\.(?:MustCompile|MustCompilePOSIX|Compile)\s*\(")

FIELD_LABEL = re.compile(r"^\s*([A-Za-z_]\w*)\s*:")

# Hằng dữ liệu dạng danh sách từ (danh sách từ chức/đại từ Trung chẳng hạn).
WORDLIST_NAME = re.compile(
    r"^\s*(?:const|var)\s+([A-Za-z_]\w*(?:Stop|Stops|Words|Chars|List|Set|Dict)\w*)\s*=\s*"
)


class Literal:
    """Một chuỗi literal (hoặc comment) trong file Go."""

    __slots__ = ("path", "line", "body", "kind", "reason")

    def __init__(self, path: str, line: int, body: str) -> None:
        self.path = path
        self.line = line
        self.body = body
        self.kind = "violation"
        self.reason = ""

    def __repr__(self) -> str:  # pragma: no cover - chỉ phục vụ debug
        return f"<{self.path}:{self.line} {self.kind}>"


# ------------------------------------------------------------------ quét -- #


def scan_literals(text: str) -> list[tuple[int, str]]:
    """Trả về [(dòng, nội dung literal)] cho mọi chuỗi literal trong file Go.

    Bỏ qua comment `//` và `/* */`.  Xử lý cả literal thường (`"..."`) và
    raw string (`` `...` ``).
    """
    literals: list[tuple[int, str]] = []
    i, n = 0, len(text)
    line = 1
    while i < n:
        ch = text[i]
        if ch == "\n":
            line += 1
            i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "/":
            end = text.find("\n", i)
            i = n if end == -1 else end
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "*":
            end = text.find("*/", i + 2)
            if end == -1:
                break
            line += text.count("\n", i, end)
            i = end + 2
            continue
        if ch == "`":
            end = text.find("`", i + 1)
            if end == -1:
                break
            body = text[i : end + 1]
            literals.append((line, body))
            line += body.count("\n")
            i = end + 1
            continue
        if ch == '"':
            j = i + 1
            while j < n:
                if text[j] == "\\":
                    j += 2
                    continue
                if text[j] == '"':
                    j += 1
                    break
                if text[j] == "\n":
                    break
                j += 1
            body = text[i:j]
            literals.append((line, body))
            line += body.count("\n")
            i = j
            continue
        i += 1
    return literals


def scan_comments(text: str) -> list[tuple[int, str]]:
    """Trả về [(dòng, nội dung comment)] cho comment `//` và `/* */`."""
    comments: list[tuple[int, str]] = []
    i, n = 0, len(text)
    line = 1
    while i < n:
        ch = text[i]
        if ch == "\n":
            line += 1
            i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "/":
            end = text.find("\n", i)
            end = n if end == -1 else end
            body = text[i:end]
            comments.append((line, body))
            line += body.count("\n")
            i = end
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "*":
            end = text.find("*/", i + 2)
            end = n if end == -1 else end + 2
            body = text[i:end]
            comments.append((line, body))
            line += body.count("\n")
            i = end
            continue
        if ch == "`":
            end = text.find("`", i + 1)
            if end == -1:
                break
            body = text[i : end + 1]
            line += body.count("\n")
            i = end + 1
            continue
        if ch == '"':
            j = i + 1
            while j < n:
                if text[j] == "\\":
                    j += 2
                    continue
                if text[j] == '"':
                    j += 1
                    break
                if text[j] == "\n":
                    break
                j += 1
            body = text[i:j]
            line += body.count("\n")
            i = j
            continue
        i += 1
    return comments


# ------------------------------------------------------------ phân loại -- #


def zh_table_ranges(lines: list[str]) -> list[tuple[int, int, str]]:
    """Khoảng dòng của các bảng ngữ zh (biến `xxxZH` hoặc hằng `xxxZH`).

    Nhận diện:
      * `var xxxZH = &T{...}` / `var xxxZH = T{...}` / `var xxxZH = map[K]V{...}`
      * `const xxxZH = `...`` (raw string nhiều dòng)
      * các khối lồng nhau bên trong.
    """
    ranges: list[tuple[int, int, str]] = []
    i = 0
    while i < len(lines):
        m = ZH_IDENT.search(lines[i])
        if not m or lines[i].lstrip().startswith("//"):
            i += 1
            continue
        name = m.group(1)
        rest = lines[i][m.end() :]
        # chỉ coi là khai báo bảng nếu phần còn lại gán một giá trị
        if not re.match(r"\s*(?:\w+\s*)?[:=]\s*(?:&)?[\w\.\[\]\*]*\s*[\{\[]", rest):
            i += 1
            continue
        depth = 0
        opened = False
        j = i
        while j < len(lines):
            depth += lines[j].count("{") + lines[j].count("[")
            depth -= lines[j].count("}") + lines[j].count("]")
            if "{" in lines[j] or "[" in lines[j]:
                opened = True
            if opened and depth <= 0:
                break
            j += 1
        ranges.append((i + 1, j + 1, name))
        i = j + 1
    return ranges


def zh_const_ranges(lines: list[str]) -> list[tuple[int, int, str]]:
    """Khoảng dòng của hằng raw-string có đuôi `ZH` (vd `coCreateSystemPromptZH`)."""
    ranges: list[tuple[int, int, str]] = []
    i = 0
    while i < len(lines):
        m = ZH_IDENT.search(lines[i])
        if not m or lines[i].lstrip().startswith("//"):
            i += 1
            continue
        rest = lines[i][m.end() :]
        if not re.match(r"\s*(?:\w+\s*)?=\s*`", rest):
            i += 1
            continue
        name = m.group(1)
        j = i
        while j < len(lines):
            if lines[j].rstrip().endswith("`"):
                break
            j += 1
        ranges.append((i + 1, j + 1, name))
        i = j + 1
    return ranges


def call_ranges(lines: list[str], pattern: re.Pattern[str]) -> list[tuple[int, int]]:
    """Khoảng dòng của các lời gọi hàm khớp với `pattern` (có thể trải nhiều dòng)."""
    ranges: list[tuple[int, int]] = []
    for idx, line in enumerate(lines):
        if pattern.search(line):
            depth = line.count("(") - line.count(")")
            end = idx
            while depth > 0 and end + 1 < len(lines):
                end += 1
                depth += lines[end].count("(") - lines[end].count(")")
            ranges.append((idx + 1, end + 1))
    return ranges


def case_ranges(lines: list[str]) -> list[tuple[int, int]]:
    """Khoảng dòng thân từng nhánh `case` của một `switch`."""
    ranges: list[tuple[int, int]] = []
    i = 0
    while i < len(lines):
        if re.match(r"^\s*case\b", lines[i]):
            start = i + 1
            j = i + 1
            while j < len(lines) and not re.match(
                r"^\s*(case\b|default\s*:|\})", lines[j]
            ):
                j += 1
            ranges.append((start, j + 1))
            i = j
        else:
            i += 1
    return ranges


def in_ranges(line: int, ranges: list[tuple[int, int]]) -> bool:
    return any(start <= line <= end for start, end in ranges)


def nearest_field_label(lines: list[str], line: int) -> str:
    """Nhãn trường gần nhất phía trên `line` (để bắt trường nhiều dòng)."""
    idx = line - 2  # dòng trước (0-based)
    while idx >= 0:
        src = lines[idx]
        if src.strip() and not src.lstrip().startswith("//"):
            m = FIELD_LABEL.match(src)
            if m:
                return m.group(1)
            if src.rstrip().endswith(("}", ")", ",")):
                return ""
        idx -= 1
    return ""


def is_chinese_wordlist(lines: list[str], line: int, body: str) -> bool:
    """Hằng dữ liệu chỉ gồm chữ Hán (danh sách từ chức/đại từ Trung)."""
    src = lines[line - 1] if 0 < line <= len(lines) else ""
    if not WORDLIST_NAME.match(src):
        return False
    inner = body.strip('"').strip("`")
    if not inner:
        return False
    return all("一" <= ch <= "鿿" for ch in inner)


def classify_literal(
    path: str,
    line: int,
    body: str,
    lines: list[str],
    zh_tables: list[tuple[int, int, str]],
) -> Literal:
    """Phân loại một literal chứa CJK thành `preserved` hoặc `violation`."""
    regexes = call_ranges(lines, REGEXP_CALL)
    matches = call_ranges(lines, MATCH_CALL)

    lit = Literal(path, line, body)

    # 1. nằm trong bảng/hằng ngữ zh
    hit = next((name for start, end, name in zh_tables if start <= line <= end), None)
    if hit:
        lit.kind = "preserved"
        lit.reason = f"bảng ngữ zh `{hit}`"
        return lit

    # 2. trường `VI:` / `ZH:` (bảng chuyển ngữ dạng key-value), kể cả các
    #    dòng nối tiếp của một trường nhiều dòng.
    src = lines[line - 1] if 0 < line <= len(lines) else ""
    own = FIELD_LABEL.match(src)
    field = own.group(1) if own else nearest_field_label(lines, line)
    if field in ("VI", "ZH"):
        lit.kind = "preserved"
        lit.reason = f"trường `{field}:` trong bảng chuyển ngữ"
        return lit

    # 3. nhãn tiếng Việt có nhúng mẫu tiếng Trung (vd 『不是…(而)是…』):
    #    nhãn đã dịch, chỉ giữ mảnh Trung là dấu hiệu cần phát hiện.
    if VIETNESE.search(body):
        lit.kind = "preserved"
        lit.reason = "nhãn tiếng Việt nhúng mẫu tiếng Trung"
        return lit

    # 4. hằng dữ liệu tiếng Trung (danh sách từ chức/đại từ) dùng để phân
    #    tích văn bản Trung — không phải chuỗi hiển thị.
    if is_chinese_wordlist(lines, line, body):
        lit.kind = "preserved"
        lit.reason = "hằng dữ liệu tiếng Trung dùng để phân tích"
        return lit

    # 5. regex bắt mã Trung — cần giữ để đọc đầu vào tiếng Trung
    if in_ranges(line, regexes):
        lit.kind = "preserved"
        lit.reason = "regex bắt mã Trung"
        return lit

    # 6. nhánh so khớp song ngữ: chuỗi Trung nằm trong lời gọi so khớp
    #    và có chuỗi Việt đi kèm trong cùng nhánh `case`.
    if in_ranges(line, matches):
        window_lines: list[str] = []
        for start, end in matches:
            if start <= line <= end:
                window_lines.extend(lines[start - 1 : end])
        for start, end in case_ranges(lines):
            if start <= line <= end:
                window_lines.extend(lines[start - 1 : end])
        if VIETNESE.search("\n".join(window_lines)):
            lit.kind = "preserved"
            lit.reason = "nhánh so khớp song ngữ"
            return lit

    lit.kind = "violation"
    lit.reason = "chuỗi sót thật"
    return lit


# ------------------------------------------------------------------ quét -- #


def protected(path: str, entries: list[str]) -> bool:
    for entry in entries:
        if entry.endswith("/"):
            if path.startswith(entry):
                return True
        elif path == entry:
            return True
    return False


def git_ls_files(scopes: list[str]) -> list[str]:
    proc = subprocess.run(
        ["git", "ls-files", "--"] + scopes,
        check=True,
        capture_output=True,
        text=True,
    )
    return proc.stdout.split()


def scan(
    scopes: list[str],
    denylist: list[str],
    production_only: bool,
    strict_comments: bool = False,
) -> tuple[list[Literal], int, int]:
    """Quét phạm vi.

    Trả về (literal đã phân loại, số file đã quét, số file được bảo vệ).
    """
    found: list[Literal] = []
    scanned = 0
    skipped = 0
    for path in git_ls_files(scopes):
        if not path.endswith(".go"):
            continue
        if protected(path, denylist):
            skipped += 1
            continue
        if production_only and path.endswith("_test.go"):
            continue
        scanned += 1
        try:
            text = open(path, encoding="utf-8", errors="replace").read()
        except OSError:
            continue
        lines = text.splitlines()
        zh_tables = zh_table_ranges(lines) + zh_const_ranges(lines)
        for line, body in scan_literals(text):
            if not CJK.search(body):
                continue
            found.append(classify_literal(path, line, body, lines, zh_tables))
        if strict_comments:
            for line, body in scan_comments(text):
                if not CJK.search(body):
                    continue
                lit = Literal(path, line, body)
                if not in_ranges(line, [(s, e) for s, e, _ in zh_tables]):
                    lit.kind = "violation"
                    lit.reason = "ký tự CJK còn sót trong comment"
                    found.append(lit)
    return found, scanned, skipped


# -------------------------------------------------------------- baseline -- #


def baseline_path_counters(found: list[Literal]) -> dict[str, Counter]:
    """Đếm literal theo (path, body) để baseline bám theo nội dung, không theo dòng."""
    counters: dict[str, Counter] = {}
    for lit in found:
        if lit.kind != "violation":
            continue
        counters.setdefault(lit.path, Counter())[lit.body] += 1
    return counters


def load_baseline(path: str) -> dict[str, Counter]:
    if not os.path.exists(path):
        return {}
    with open(path, encoding="utf-8") as fh:
        data = json.load(fh)
    return {p: Counter(v.get("literals", [])) for p, v in data.get("files", {}).items()}


def write_baseline(path: str, counters: dict[str, Counter]) -> None:
    payload = {
        "version": 1,
        "note": (
            "Baseline cổng Việt hóa: các literal CJK còn sót đã biết. "
            "CI chỉ đỏ khi có literal MỚI chưa có trong danh sách này. "
            "Sinh lại bằng: bash scripts/check-vi-port.sh --update-baseline <file>"
        ),
        "files": {
            p: {"literals": sorted(c.elements())}
            for p, c in sorted(counters.items())
            if c
        },
    }
    directory = os.path.dirname(os.path.abspath(path))
    if directory:
        os.makedirs(directory, exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(payload, fh, ensure_ascii=False, indent=1, sort_keys=True)
        fh.write("\n")


# ------------------------------------------------------------------- cli -- #


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(
        prog="vi_port_scan.py",
        description="Quét chuỗi literal CJK còn sót trong mã Go của ainovel-cli.",
    )
    ap.add_argument(
        "--scope",
        nargs="+",
        required=True,
        help="thư mục cần quét (đường dẫn tương đối so với repo root)",
    )
    ap.add_argument(
        "--denylist",
        default="",
        help="đường dẫn luôn được bỏ qua, phân tách bằng '|'",
    )
    ap.add_argument(
        "--production",
        action="store_true",
        help="chỉ quét file không phải _test.go",
    )
    ap.add_argument(
        "--strict-comments",
        action="store_true",
        help="coi ký tự CJK trong comment Go cũng là vi phạm",
    )
    ap.add_argument(
        "--baseline",
        help="file baseline: literal đã biết được bỏ qua, literal mới thì đỏ",
    )
    ap.add_argument(
        "--update-baseline",
        help="ghi lại baseline hiện tại rồi thoát",
    )
    ap.add_argument(
        "--json",
        action="store_true",
        help="xuất kết quả dạng JSON thay vì văn bản",
    )
    args = ap.parse_args(argv)

    root = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    os.chdir(root)

    denylist = [p for p in args.denylist.split("|") if p]
    found, scanned, skipped = scan(
        args.scope, denylist, args.production, args.strict_comments
    )

    if args.update_baseline:
        write_baseline(args.update_baseline, baseline_path_counters(found))
        print(f"Đã ghi baseline vào {args.update_baseline}")
        return 0

    preserved = [l for l in found if l.kind == "preserved"]
    violations = [l for l in found if l.kind == "violation"]

    known: list[Literal] = []
    new: list[Literal] = []
    if args.baseline:
        base = load_baseline(args.baseline)
        remaining = {p: Counter(c) for p, c in base.items()}
        for lit in violations:
            counter = remaining.get(lit.path)
            if counter and counter[lit.body] > 0:
                counter[lit.body] -= 1
                known.append(lit)
            else:
                new.append(lit)
    else:
        new = violations

    if args.json:
        print(
            json.dumps(
                {
                    "preserved": [
                        {
                            "path": l.path,
                            "line": l.line,
                            "reason": l.reason,
                            "text": l.body,
                        }
                        for l in preserved
                    ],
                    "known": [
                        {"path": l.path, "line": l.line, "text": l.body} for l in known
                    ],
                    "new": [
                        {"path": l.path, "line": l.line, "text": l.body} for l in new
                    ],
                },
                ensure_ascii=False,
                indent=1,
            )
        )
        return 1 if new else 0

    for lit in preserved:
        print(f"[giữ] {lit.path}:{lit.line}: {lit.reason}")
    for lit in known:
        print(f"[đã biết] {lit.path}:{lit.line}: {lit.body}")
    for lit in new:
        print(f"{lit.path}:{lit.line}: chuỗi literal CJK còn sót: {lit.body}")
    print("")
    print(
        "Đã quét %d file Go trong phạm vi đang bật, bỏ qua %d file được bảo vệ."
        % (scanned, skipped)
    )
    print(
        "Đã thấy %d literal CJK: %d giữ nguyên, %d đã biết, %d mới."
        % (len(found), len(preserved), len(known), len(new))
    )
    if new:
        print(f"Phát hiện {len(new)} vi phạm CJK mới.")
        return 1
    print("Không có chuỗi literal CJK mới.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
