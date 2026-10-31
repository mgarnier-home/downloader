import { formatBytes, formatEta, formatSpeed, packSummary, seasons } from './format';

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

describe('seasons', () => {
  it('liste les saisons distinctes, triées', () => {
    expect(seasons(['series/A/S02/A.S02.E01.mkv', 'series/A/S01/A.S01.E01.mkv', 'series/A/S02/Bonus/x.mkv']))
      .toBe('Saisons 1, 2');
    expect(seasons(['series/A/S01/A.S01.E01.mkv'])).toBe('Saison 1');
    expect(seasons(['series/A/Bonus/x.mkv', 'series/A'])).toBe('');
  });
});

describe('packSummary', () => {
  it('commence par les saisons quand le pack liste ses vidéos', () => {
    expect(packSummary({ placed: 1, present: 0, bonus: 0, items: [{ dest: 'series/A/S03/A.S03.E01.mkv', result: 'placed' }] }))
      .toBe('Saison 3 · 1 épisode rangé');
  });

  it('donne le bilan complet', () => {
    expect(packSummary({ placed: 62, present: 2, bonus: 1 })).toBe('62 épisodes rangés · 2 déjà présents · 1 bonus');
  });

  it('accorde au singulier et omet les zéros secondaires', () => {
    expect(packSummary({ placed: 1, present: 0, bonus: 0 })).toBe('1 épisode rangé');
    expect(packSummary({ placed: 0, present: 3, bonus: 0 })).toBe('0 épisode rangé · 3 déjà présents');
  });
});
