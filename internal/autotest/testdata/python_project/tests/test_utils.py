"""
Tests for utility functions
"""
import pytest
from src.utils import add, multiply


class TestMathUtilities:
    """Test math utility functions."""

    @pytest.mark.parametrize("a,b,expected", [
        (2, 3, 5),
        (-2, -3, -5),
        (0, 0, 0),
    ])
    def test_add(self, a, b, expected):
        """Test adding two numbers."""
        assert add(a, b) == expected

    @pytest.mark.parametrize("a,b,expected", [
        (2, 3, 6),
        (5, 0, 0),
        (-2, 3, -6),
    ])
    def test_multiply(self, a, b, expected):
        """Test multiplying two numbers."""
        assert multiply(a, b) == expected
