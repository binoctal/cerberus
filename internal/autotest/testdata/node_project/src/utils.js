/**
 * Utility functions for AutoTest integration testing
 */

// This function is covered by tests
export function add(a, b) {
  return a + b;
}

// This function is NOT covered by tests (gap for AutoTest)
export function subtract(a, b) {
  return a - b;
}

// This function is covered by tests
export function multiply(a, b) {
  return a * b;
}

// This class is NOT covered by tests (gap for AutoTest)
export class Calculator {
  constructor() {
    this.result = 0;
  }

  add(value) {
    this.result += value;
  }

  getResult() {
    return this.result;
  }
}
