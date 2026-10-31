import { formatBytes, formatEta, formatSpeed } from './format';

describe('format', () => {
  it('formatBytes', () => {
    expect(formatBytes(4_800_000_000)).toBe('4,8 Go');
    expect(formatBytes(950_000_000)).toBe('950 Mo');
    expect(formatBytes(4_100_000_000_000)).toBe('4,1 To');
    expect(formatBytes(0)).toBe('0 o');
  });
  it('formatSpeed', () => {
    expect(formatSpeed(86_000_000)).toBe('86 Mo/s');
  });
  it('formatEta', () => {
    expect(formatEta(2_000_000_000, 86_000_000)).toBe('23 s');
    expect(formatEta(3_900_000_000_000, 1_000_000_000)).toBe('1 h 05');
    expect(formatEta(180_000_000, 1_000_000)).toBe('3 min');
    expect(formatEta(10, 0)).toBe('');
  });
});
