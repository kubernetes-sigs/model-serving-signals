# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import unittest
from types import SimpleNamespace

from kmonitor_bridge import FAMILIES, Snapshots

NAME = "py_rtp_framework_rt_seconds"


def batch(count=2, timestamp=100, process="frontend-1", total=0.03):
    # One sample at .01s and one at .02s (both exactly on boundaries).
    rows = [
        ("bucket", count if b >= 0.02 else min(count, 1), f" le={b}")
        for b in FAMILIES[NAME]
    ]
    rows += [("bucket", count, " le=+Inf"), ("sum", total, ""), ("count", count, "")]
    return [
        SimpleNamespace(
            headers={"topic": "py_kmonitor"},
            body=(
                f'{NAME}_{suffix} {timestamp} {value} process_instance_id={process} hippo_app= model=a"b\\c{extra}'
            ).encode(),
        )
        for suffix, value, extra in rows
    ]


class BridgeTest(unittest.TestCase):
    def test_snapshot_not_added_twice_and_processes_preserved(self):
        store = Snapshots()
        store.accept(batch(), now=100)
        first = store.render(now=100)
        store.accept(batch(), now=100)
        self.assertEqual(first, store.render(now=100))
        store.accept(batch(process="frontend-2"), now=100)
        result = store.render(now=100).decode()
        self.assertEqual(result.count(f"{NAME}_count"), 2)
        self.assertIn('le="0.01"} 1', result)
        self.assertIn('le="0.02"} 2', result)
        self.assertIn('le="+Inf"} 2', result)
        self.assertIn('hippo_app=""', result)
        self.assertIn('model="a\\"b\\\\c"', result)

    def test_incomplete_or_inconsistent_batches_are_atomic(self):
        for mode in (
            "missing bucket",
            "missing sum",
            "duplicate",
            "inconsistent",
            "negative",
            "nan",
            "missing process",
        ):
            with self.subTest(mode=mode):
                store = Snapshots()
                store.accept(batch(), now=100)
                original = store.render(now=100)
                events = batch(timestamp=101)
                if mode == "missing bucket":
                    events.pop(0)
                elif mode == "missing sum":
                    events.pop(-2)
                elif mode == "duplicate":
                    events.append(events[0])
                elif mode == "inconsistent":
                    events[0].body = events[0].body.replace(b"101 1 ", b"101 3 ")
                elif mode == "negative":
                    events[0].body = events[0].body.replace(b"101 1 ", b"101 -1 ")
                elif mode == "nan":
                    events[-2].body = events[-2].body.replace(b"101 0.03 ", b"101 nan ")
                elif mode == "missing process":
                    events[0].body = events[0].body.replace(
                        b"process_instance_id=frontend-1 ", b""
                    )
                with self.assertRaises(ValueError):
                    store.accept(events, now=101)
                self.assertEqual(original, store.render(now=101))

    def test_delayed_snapshot_cannot_regress_counts(self):
        store = Snapshots()
        store.accept(batch(count=3, timestamp=102, total=0.05), now=102)
        original = store.render(now=102)
        store.accept(batch(timestamp=101), now=102)
        store.accept(batch(timestamp=102), now=102)
        self.assertEqual(original, store.render(now=102))

    def test_expiry_and_series_limit(self):
        store = Snapshots(max_age=5, max_series=1)
        store.accept(batch(), now=100)
        with self.assertRaises(ValueError):
            store.accept(batch(process="another"), now=100)
        self.assertEqual(store.render(now=106), b"\n")
        with self.assertRaises(ValueError):
            store.accept(batch(), now=106)
        store.accept(batch(timestamp=106, process="another"), now=106)
        self.assertIn(b"another", store.render(now=106))

    def test_unknown_metrics_ignored(self):
        store = Snapshots()
        store.accept(
            [
                SimpleNamespace(
                    headers={"topic": "py_kmonitor"}, body=b"unrelated 100 1"
                )
            ],
            now=100,
        )
        self.assertEqual(store.render(now=100), b"\n")


if __name__ == "__main__":
    unittest.main()
