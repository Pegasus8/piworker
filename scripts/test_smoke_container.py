"""Unit checks for the container smoke test's helpers (the run itself needs Docker)."""
import unittest

import smoke_container


class HealthTests(unittest.TestCase):
    def test_health_accepts_extra_fields_and_requires_healthy_status(self):
        # /api/health also answers the process's instance (ADR-0032).
        self.assertTrue(smoke_container.is_healthy({"status": "healthy", "instance": "abc"}))
        self.assertFalse(smoke_container.is_healthy({"status": "unhealthy", "instance": "abc"}))
        self.assertFalse(smoke_container.is_healthy(["healthy"]))


if __name__ == "__main__":
    unittest.main()
