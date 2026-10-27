#!/usr/bin/env python3
"""Self-test cho bộ quét Việt hóa (scripts/vi_port_scan.py).

Chạy trực tiếp:

    python3 scripts/vi_port_scan_test.py

hoặc qua cổng:

    bash scripts/check-vi-port.sh --self-test

Cổng kiểm tra chính nó cũng là mã, nên cần một bộ test bảo vệ nó: nếu logic
baseline hỏng thì cổng có thể "xanh giả" — báo là đã biết mọi thứ, hoặc xoá
mất danh sách đã ghi nhận.
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCANNER = Path(__file__).with_name("vi_port_scan.py")
COMMENT_DIFF = Path(__file__).with_name("comment_only_diff.py")


def load_scanner():
    spec = importlib.util.spec_from_file_location("vi_port_scan", SCANNER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_comment_diff():
    spec = importlib.util.spec_from_file_location("comment_only_diff", COMMENT_DIFF)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


scan_mod = load_scanner()
diff_mod = load_comment_diff()

SAMPLE = """package demo

// 这是中文注释
func demo() string {
	return "还有中文"
}
"""


def make_repo(root: Path, source: str = SAMPLE) -> Path:
    """Tạo một repo git tối giản chứa một file Go."""
    repo = root / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    (repo / "sample.go").write_text(source, encoding="utf-8")
    subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
    subprocess.run(["git", "add", "sample.go"], cwd=repo, check=True)
    return repo


class BaselineTest(unittest.TestCase):
    def test_legacy_v1_baseline_still_loads(self):
        """File baseline phiên bản 1 (chưa có mục comments) vẫn nạp được."""
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "legacy.json"
            path.write_text(
                json.dumps(
                    {
                        "version": 1,
                        "note": "legacy",
                        "files": {"internal/host/texts.go": {"literals": ["测试"]}},
                    },
                    ensure_ascii=False,
                ),
                encoding="utf-8",
            )
            loaded = scan_mod.load_baseline(str(path))
            self.assertEqual(loaded["internal/host/texts.go"]["测试"], 1)

    def test_write_baseline_splits_literals_and_comments(self):
        """Baseline ghi riêng mục literal và mục comment."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            baseline = Path(tmp) / "baseline.json"
            cwd = os.getcwd()
            os.chdir(repo)
            try:
                found, comments, _, _ = scan_mod.scan(["."], [], True, False)
                literals, comment_hits = scan_mod.baseline_path_counters(
                    found, comments
                )
                scan_mod.write_baseline(
                    str(baseline), literals, comment_hits
                )
            finally:
                os.chdir(cwd)

            data = json.loads(baseline.read_text(encoding="utf-8"))
            self.assertEqual(data["version"], 2)
            self.assertIn("comments", data)
            self.assertIn("sample.go", data["comments"])
            self.assertEqual(
                data["comments"]["sample.go"]["comments"], ["// 这是中文注释"]
            )
            self.assertEqual(data["files"]["sample.go"]["literals"], ['"还有中文"'])

    def test_update_without_strict_keeps_comments(self):
        """Chạy --update-baseline (không bật strict) không được xoá mục comment.

        Đây là bẫy đã gặp: baseline ghi đè toàn bộ file, nên comment đã biết
        biến mất sau lần cập nhật đầu tiên và cổng strict lại đỏ trần.
        """
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            baseline = Path(tmp) / "baseline.json"
            cwd = os.getcwd()
            os.chdir(repo)
            try:
                for _ in range(2):  # cập nhật hai lần liên tiếp
                    found, comments, _, _ = scan_mod.scan(["."], [], True, False)
                    literals, comment_hits = scan_mod.baseline_path_counters(
                        found, comments
                    )
                    scan_mod.write_baseline(
                        str(baseline), literals, comment_hits
                    )
                    data = json.loads(baseline.read_text(encoding="utf-8"))
                    self.assertEqual(
                        data["comments"]["sample.go"]["comments"],
                        ["// 这是中文注释"],
                        "mục comment phải còn sau mỗi lần cập nhật baseline",
                    )
            finally:
                os.chdir(cwd)

    def test_strict_scan_reports_comment_and_baseline_covers_it(self):
        """Bật strict thì comment thành vi phạm; có baseline thì thành đã biết."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            baseline = Path(tmp) / "baseline.json"
            cwd = os.getcwd()
            os.chdir(repo)
            try:
                found, comments, _, _ = scan_mod.scan(["."], [], True, True)
                comment_violations = [
                    lit for lit in found if lit.is_comment and lit.kind == "violation"
                ]
                self.assertEqual(len(comment_violations), 1)

                literals, comment_hits = scan_mod.baseline_path_counters(
                    found, comments
                )
                scan_mod.write_baseline(str(baseline), literals, comment_hits)

                loaded = scan_mod.load_baseline(str(baseline))
                counter = loaded.get("sample.go")
                self.assertIsNotNone(counter)
                self.assertEqual(counter["// 这是中文注释"], 1)
            finally:
                os.chdir(cwd)

    def test_new_comment_is_not_grandfathered(self):
        """Comment mới (không có trong baseline) phải bị báo là mới."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            baseline = Path(tmp) / "baseline.json"
            baseline.write_text(
                json.dumps({"version": 2, "files": {}, "comments": {}}, ensure_ascii=False),
                encoding="utf-8",
            )
            cwd = os.getcwd()
            os.chdir(repo)
            try:
                found, _, _, _ = scan_mod.scan(["."], [], True, True)
                loaded = scan_mod.load_baseline(str(baseline))
                remaining = {p: scan_mod.Counter(c) for p, c in loaded.items()}
                new = []
                for lit in found:
                    if lit.kind != "violation":
                        continue
                    counter = remaining.get(lit.path)
                    if counter and counter[lit.body] > 0:
                        counter[lit.body] -= 1
                    else:
                        new.append(lit)
                self.assertTrue(
                    any(lit.is_comment for lit in new),
                    "comment mới phải bị báo, không được grandfather",
                )
            finally:
                os.chdir(cwd)

    def test_comments_only_filters_literals_from_report(self):
        """--comments-only chỉ báo comment, không báo literal.

        Khi dịch backlog comment, mỗi dòng "giữ nguyên" của literal là nhiễu:
        người đọc dễ tưởng còn việc phải làm.
        """
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            cwd = os.getcwd()
            os.chdir(repo)
            try:
                buffer = io.StringIO()
                with contextlib.redirect_stdout(buffer):
                    status = scan_mod.main(
                        [
                            "--scope",
                            ".",
                            "--production",
                            "--comments-only",
                            "--update-baseline",
                            str(Path(tmp) / "baseline.json"),
                        ]
                    )
                self.assertEqual(status, 0)
                self.assertIn("Đã ghi baseline", buffer.getvalue())

                buffer = io.StringIO()
                with contextlib.redirect_stdout(buffer):
                    status = scan_mod.main(
                        ["--scope", ".", "--production", "--comments-only"]
                    )
                output = buffer.getvalue()
                self.assertEqual(status, 1)  # chưa có baseline nên comment là mới
                self.assertIn("comment CJK", output)
                self.assertNotIn("还有中文", output)
                self.assertNotIn("[giữ]", output)
            finally:
                os.chdir(cwd)


GO_SAMPLE = """package demo

// 这是中文注释
func demo() string {
	return "还有中文" // ghi chú cuối dòng
}
"""


def commit_all(repo: Path, message: str) -> None:
    """Commit mọi thay đổi trong repo tạm (không phụ thuộc git config toàn cục)."""
    identity = [
        "-c",
        "user.name=Toolsmith",
        "-c",
        "user.email=toolsmith@example.invalid",
        "-c",
        "commit.gpgsign=false",
    ]
    subprocess.run(["git", *identity, "add", "-A"], cwd=repo, check=True)
    subprocess.run(
        ["git", *identity, "commit", "-q", "-m", message], cwd=repo, check=True
    )


def run_diff(repo: Path, rng: str) -> subprocess.CompletedProcess:
    """Chạy comment_only_diff.py trong repo tạm, trả về kết quả."""
    return subprocess.run(
        [sys.executable, str(COMMENT_DIFF), rng],
        cwd=repo,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )


class CommentOnlyDiffTest(unittest.TestCase):
    """Kiểm tra trình xác minh "diff chỉ đổi comment".

    Đây là công cụ Chief dùng để chấp nhận nhanh branch dịch comment: thay vì
    đọc diff bằng mắt, chạy máy và có kết luận khách quan.
    """

    def make_repo(self, root: Path) -> tuple[Path, str]:
        repo = root / "repo"
        repo.mkdir(parents=True, exist_ok=True)
        (repo / "demo.go").write_text(GO_SAMPLE, encoding="utf-8")
        subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
        commit_all(repo, "khởi tạo")
        base = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=repo,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        return repo, base

    def test_code_tokens_ignore_comments(self):
        """Comment không đổi danh sách token của phần mã."""
        self.assertEqual(
            diff_mod.code_tokens("x := 1 // note\n"),
            diff_mod.code_tokens("x := 1\n"),
        )
        self.assertEqual(
            diff_mod.code_tokens("f(a /*x*/, b)\n"),
            diff_mod.code_tokens("f(a, b)\n"),
        )

    def test_code_tokens_keep_string_interior(self):
        """Khoảng trắng bên trong chuỗi là thay đổi thật, phải khác token."""
        self.assertNotEqual(
            diff_mod.code_tokens('s := "a  b"\n'),
            diff_mod.code_tokens('s := "a b"\n'),
        )

    def test_code_tokens_ignore_line_numbers(self):
        """Số dòng không thuộc về mã: comment dài thêm một dòng là lệch dòng.

        Đây là bẫt đã gặp: so sánh kèm số dòng thì mọi comment viết lại
        thành hai dòng đều bị báo nhầm là "mã đã thay đổi".
        """
        one_line = diff_mod.code_tokens("// ghi chú\nx := 1\n")
        two_lines = diff_mod.code_tokens("// ghi chú\n// viết lại\nx := 1\n")
        self.assertNotEqual(one_line, two_lines)  # số dòng khác
        self.assertEqual(
            diff_mod.token_texts(one_line), diff_mod.token_texts(two_lines)
        )  # nhưng mã thì không khác

    def test_comment_reflow_passes(self):
        """Comment một dòng viết lại thành hai dòng vẫn là comment-only."""
        with tempfile.TemporaryDirectory() as tmp:
            repo, base = self.make_repo(Path(tmp))
            path = repo / "demo.go"
            path.write_text(
                GO_SAMPLE.replace(
                    "// 这是中文注释",
                    "// Ghi chú tiếng Việt\n// viết trên hai dòng",
                ),
                encoding="utf-8",
            )
            commit_all(repo, "viết lại comment")

            proc = run_diff(repo, f"{base}..HEAD")
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)

    def test_comment_only_change_passes(self):
        """Chỉ đổi nội dung comment ⇒ không vi phạm, mã thoát 0."""
        with tempfile.TemporaryDirectory() as tmp:
            repo, base = self.make_repo(Path(tmp))
            path = repo / "demo.go"
            path.write_text(
                GO_SAMPLE.replace("// 这是中文注释", "// Ghi chú tiếng Việt"),
                encoding="utf-8",
            )
            commit_all(repo, "đổi comment")

            proc = run_diff(repo, f"{base}..HEAD")
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            self.assertIn("chỉ nằm trong comment", proc.stdout)

    def test_trailing_comment_removal_passes(self):
        """Bỏ hẳn comment cuối dòng vẫn là thay đổi comment-only."""
        with tempfile.TemporaryDirectory() as tmp:
            repo, base = self.make_repo(Path(tmp))
            path = repo / "demo.go"
            path.write_text(
                GO_SAMPLE.replace(" // ghi chú cuối dòng", ""), encoding="utf-8"
            )
            commit_all(repo, "bỏ comment cuối dòng")

            proc = run_diff(repo, f"{base}..HEAD")
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)

    def test_code_change_is_flagged(self):
        """Đổi literal trong chuỗi ⇒ báo 'mã đã thay đổi', mã thoát 1."""
        with tempfile.TemporaryDirectory() as tmp:
            repo, base = self.make_repo(Path(tmp))
            path = repo / "demo.go"
            path.write_text(
                GO_SAMPLE.replace('"还有中文"', '"还有中文!"'), encoding="utf-8"
            )
            commit_all(repo, "đổi literal")

            proc = run_diff(repo, f"{base}..HEAD")
            self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
            self.assertIn("mã đã thay đổi", proc.stdout)
            self.assertIn("demo.go", proc.stdout)

    def test_non_go_file_change_is_flagged(self):
        """Đụng file không phải mã Go ⇒ báo 'file ngoài phạm vi comment'."""
        with tempfile.TemporaryDirectory() as tmp:
            repo, base = self.make_repo(Path(tmp))
            (repo / "notes.json").write_text('{"a": 1}\n', encoding="utf-8")
            commit_all(repo, "thêm file json")

            proc = run_diff(repo, f"{base}..HEAD")
            self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
            self.assertIn("file ngoài phạm vi comment", proc.stdout)

    def test_invalid_range_is_usage_error(self):
        """Phạm vi sai dạng ⇒ mã thoát 2 (lỗi tham số)."""
        with tempfile.TemporaryDirectory() as tmp:
            repo, _ = self.make_repo(Path(tmp))
            proc = run_diff(repo, "HEAD")
            self.assertEqual(proc.returncode, 2, proc.stdout + proc.stderr)

    def test_three_dot_range_uses_merge_base(self):
        """Dạng base...head lấy merge-base làm gốc.

        Nhánh đứng sau main mà so với main thì sẽ thấy cả "thay đổi" của
        chính main — đó là thay đổi của người khác, không phải của nhánh.
        """
        with tempfile.TemporaryDirectory() as tmp:
            repo, base = self.make_repo(Path(tmp))

            # "main" đi trước bằng một commit không liên quan.
            subprocess.run(
                ["git", "checkout", "-q", "-b", "main"], cwd=repo, check=True
            )
            (repo / "other.go").write_text("package demo\n", encoding="utf-8")
            commit_all(repo, "commit trên main")
            subprocess.run(["git", "checkout", "-q", "-"], cwd=repo, check=True)

            # Nhánh hiện tại đổi mã.
            path = repo / "demo.go"
            path.write_text(
                GO_SAMPLE.replace('"还有中文"', '"还有中文!"'), encoding="utf-8"
            )
            commit_all(repo, "đổi literal")

            # So thẳng với main: thấy cả commit của main (other.go bị mất).
            proc = run_diff(repo, "main..HEAD")
            self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
            self.assertIn("other.go", proc.stdout)

            # So từ merge-base: chỉ thấy thay đổi của nhánh này.
            proc = run_diff(repo, "main...HEAD")
            self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
            self.assertIn("demo.go", proc.stdout)
            self.assertNotIn("other.go", proc.stdout)
            self.assertEqual(base, subprocess.run(
                ["git", "rev-parse", "HEAD~1"],
                cwd=repo, check=True, capture_output=True, text=True).stdout.strip())


if __name__ == "__main__":
    unittest.main(verbosity=2)