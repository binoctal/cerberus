const assert = require('assert');
const { add, subtract, multiply, divide } = require('../src/calculator');

describe('Calculator', () => {
  describe('add', () => {
    it('should add two positive numbers', () => {
      assert.strictEqual(add(2, 3), 5);
    });

    it('should add negative numbers', () => {
      assert.strictEqual(add(-2, -3), -5);
    });
  });

  describe('subtract', () => {
    it('should subtract two numbers', () => {
      assert.strictEqual(subtract(5, 3), 2);
    });
  });

  describe('multiply', () => {
    it('should multiply two numbers', () => {
      assert.strictEqual(multiply(3, 4), 12);
    });
  });

  describe('divide', () => {
    it('should divide two numbers', () => {
      assert.strictEqual(divide(10, 2), 5);
    });

    it('should throw error on division by zero', () => {
      assert.throws(() => divide(10, 0), /Division by zero/);
    });
  });
});
