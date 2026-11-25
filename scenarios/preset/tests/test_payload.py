from concurrent.futures import ThreadPoolExecutor
import hashlib
import unittest

from helpers.cmd import TAIL_SIZE, PayloadGenerator


class TestPayloadGenerator(unittest.TestCase):
    def test_consecutive_payloads_shift_and_have_distinct_hashes(self):
        gen = PayloadGenerator(1)
        p1 = gen.next_payload()
        p2 = gen.next_payload()

        self.assertIsInstance(p1, bytes)
        self.assertIsInstance(p2, bytes)
        self.assertEqual(len(p1), 1024)
        self.assertEqual(len(p2), 1024)
        self.assertEqual(p1[1:], p2[:-1])
        self.assertNotEqual(p1, p2)
        self.assertNotEqual(hashlib.md5(p1).hexdigest(), hashlib.md5(p2).hexdigest())
        self.assertNotEqual(hashlib.sha256(p1).digest(), hashlib.sha256(p2).digest())

    def test_refills_buffer_after_consuming_tail(self):
        tail_size = 8
        gen = PayloadGenerator(1, tail_size=tail_size)
        window = [gen.next_payload() for _ in range(tail_size)]

        for prev, curr in zip(window, window[1:]):
            self.assertEqual(prev[1:], curr[:-1])

        after_refill = gen.next_payload()
        self.assertEqual(len(after_refill), 1024)
        self.assertNotEqual(after_refill, window[0])
        self.assertNotEqual(window[-1][1:], after_refill[:-1])

    def test_default_tail_size_matches_datagen(self):
        self.assertEqual(TAIL_SIZE, 1024)
        gen = PayloadGenerator(1)
        first = gen.next_payload()
        last = first
        for _ in range(TAIL_SIZE - 1):
            last = gen.next_payload()
        after_refill = gen.next_payload()
        self.assertEqual(len(after_refill), 1024)
        self.assertNotEqual(after_refill, first)
        self.assertNotEqual(last[1:], after_refill[:-1])

    def test_separate_instances_produce_distinct_payloads(self):
        g1 = PayloadGenerator(1)
        g2 = PayloadGenerator(1)
        self.assertNotEqual(g1.next_payload(), g2.next_payload())

    def test_zero_size(self):
        gen = PayloadGenerator('0')
        self.assertEqual(gen.next_payload(), b'')
        self.assertEqual(gen.next_payload(), b'')

    def test_invalid_arguments(self):
        for bad_size in (-1, '-1', 'not-a-number'):
            with self.subTest(size_kb=bad_size):
                with self.assertRaises(ValueError):
                    PayloadGenerator(bad_size)

        for bad_tail in (0, -1, -10):
            with self.subTest(tail_size=bad_tail):
                with self.assertRaises(ValueError):
                    PayloadGenerator(1, tail_size=bad_tail)

    def test_concurrent_calls_across_refill_boundary(self):
        tail_size = 32
        total_calls = 200
        gen = PayloadGenerator(1, tail_size=tail_size)

        with ThreadPoolExecutor(max_workers=16) as executor:
            payloads = list(executor.map(lambda _: gen.next_payload(), range(total_calls)))

        self.assertEqual(len(payloads), total_calls)
        self.assertTrue(all(len(p) == 1024 for p in payloads))
        self.assertEqual(len(set(payloads)), total_calls)


if __name__ == '__main__':
    unittest.main()
