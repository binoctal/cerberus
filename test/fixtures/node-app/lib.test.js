const { add, sub } = require('./lib');

describe('math functions', () => {
  test('add adds two numbers', () => {
    expect(add(2, 3)).toBe(5);
  });

  test('sub subtracts two numbers', () => {
    expect(sub(5, 3)).toBe(2);
  });
});
