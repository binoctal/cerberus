"""
Utility functions for AutoTest integration testing
"""


# This function is covered by tests
def add(a, b):
    """Add two numbers."""
    return a + b


# This function is NOT covered by tests (gap for AutoTest)
def subtract(a, b):
    """Subtract two numbers."""
    return a - b


# This function is covered by tests
def multiply(a, b):
    """Multiply two numbers."""
    return a * b


# This class is NOT covered by tests (gap for AutoTest)
class Calculator:
    """A simple calculator class."""

    def __init__(self):
        """Initialize the calculator."""
        self.result = 0

    def add(self, value):
        """Add a value to the result."""
        self.result += value

    def get_result(self):
        """Get the current result."""
        return self.result
