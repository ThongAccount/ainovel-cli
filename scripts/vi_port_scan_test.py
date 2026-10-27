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
import tempfile
import unittest
from pathlib import Path

SCANNER = Path(__file__).with_name("vi_port_scan.py")


def load_scanner():
    spec = importlib.util.spec_from_file_location("vi_port_scan", SCANNER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


scan_mod = load_scanner()

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


if __name__ == "__main__":
    unittest.main(verbosity=2)
