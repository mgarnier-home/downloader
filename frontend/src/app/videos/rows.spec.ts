import { AnalyzeRow, TMDBResult } from '../api/models';
import { applyCard, EditableRow, pascalCase, recompose, toEditable, toRequests, withDest } from './rows';

const boys: TMDBResult = { id: 76479, title: 'The Boys', year: 2019, animation: false, poster: '', tv: true };
const dunkirk: TMDBResult = { id: 374720, title: 'Dunkerque', year: 2017, animation: false, poster: '', tv: false };

const base: AnalyzeRow = {
  input: 'https://h/x', name: 'x.mkv', size: 100, online: true, error: '', kind: 'movie', tmdb: null,
  season: 0, episode: 0, needsReview: false, dest: 'films/X/X.mkv', exists: false, pack: false,
};
const row = (over: Partial<AnalyzeRow>): EditableRow => toEditable({ ...base, ...over });
const episodes = (): EditableRow[] =>
  Array.from({ length: 8 }, (_, i) => row({
    input: `https://h/e${i + 1}`, name: `The.Boys.S02E0${i + 1}.MULTi.mkv`, kind: 'series', tmdb: boys, season: 2,
    episode: i + 1, dest: `series/TheBoys/S02/TheBoys.S02.E0${i + 1}.mkv`,
  }));
const film = () => row({ name: 'Dunkirk.2017.mkv', kind: 'movie', tmdb: dunkirk, dest: 'films/Dunkerque/Dunkerque.mkv' });

describe('toRequests', () => {
  it('regroupe par fiche : 8 épisodes + un film donnent 2 requêtes', () => {
    const reqs = toRequests([...episodes(), film()]);
    expect(reqs.length).toBe(2);
    expect(reqs[0]).toMatchObject({ kind: 'series', title: 'The Boys', ref: 'tmdb:76479', deezer: '' });
    expect(reqs[0].files.length).toBe(8);
    expect(reqs[1]).toMatchObject({ kind: 'movie', title: 'Dunkerque', ref: 'tmdb:374720' });
    expect(reqs[1].files[0]).toEqual({
      input: 'https://h/x', name: 'Dunkirk.2017.mkv', size: 100, dest: 'films/Dunkerque/Dunkerque.mkv', overwrite: false,
    });
  });

  it('exclut une ligne hors ligne et une ligne « déjà présent » non cochée', () => {
    const rows = [film(), row({ online: false, dest: '', kind: '' }), row({ exists: true, dest: 'films/Y/Y.mkv' })];
    expect(rows[2].checked).toBe(false);
    const reqs = toRequests(rows);
    expect(reqs.length).toBe(1);
    expect(reqs[0].files.length).toBe(1);
  });

  it('envoie overwrite pour une ligne exists cochée', () => {
    const r = row({ exists: true, dest: 'films/Y/Y.mkv' });
    r.checked = true;
    expect(toRequests([r])[0].files[0].overwrite).toBe(true);
  });

  it('regroupe par nom sans fiche TMDB', () => {
    const reqs = toRequests([row({ dest: 'films/Foo/Foo.mkv' }), row({ dest: 'films/Foo/Foo.mkv' }), row({ dest: 'films/Bar/Bar.mkv' })]);
    expect(reqs.map((r) => [r.title, r.ref, r.files.length])).toEqual([['Foo', '', 2], ['Bar', '', 1]]);
  });
});

describe('recompose', () => {
  it('recompose la destination selon le type', () => {
    const r = episodes()[2];
    expect(recompose({ ...r, kind: 'series' })).toBe('series/TheBoys/S02/TheBoys.S02.E03.mkv');
    expect(recompose({ ...r, kind: 'movie' })).toBe('films/TheBoys/TheBoys.mkv');
    expect(recompose({ ...r, kind: 'anime' })).toBe('animes/TheBoys/TheBoys.mkv');
    expect(recompose({ ...r, bonus: true })).toBe('series/TheBoys/S02/Bonus/The.Boys.S02E03.MULTi.mkv');
  });
});

describe('pascalCase', () => {
  it('suit la règle du serveur', () => {
    expect(pascalCase("Spirit : l'indomptable")).toBe('SpiritLIndomptable');
    expect(pascalCase('Dune : Première partie')).toBe('DunePremierePartie');
    expect(pascalCase('Cœur')).toBe('Coeur');
  });
});

describe('applyCard', () => {
  it('recalcule la destination avec le titre de la fiche', () => {
    const r = row({ kind: 'movie', dest: 'films/Dunkirk/Dunkirk.mkv', name: 'Dunkirk.mkv' });
    expect(applyCard(r, dunkirk).dest).toBe('films/Dunkerque/Dunkerque.mkv');
  });
  it('garde une destination saisie à la main', () => {
    const r = { ...row({ dest: 'films/Mien/Mien.mkv' }), destEdited: true };
    expect(applyCard(r, dunkirk).dest).toBe('films/Mien/Mien.mkv');
  });
  it('passe en animé pour un film d’animation', () => {
    const r = row({ kind: 'movie', dest: 'films/S/S.mkv' });
    const out = applyCard(r, { ...dunkirk, title: 'Spirit', animation: true });
    expect([out.kind, out.dest]).toEqual(['anime', 'animes/Spirit/Spirit.mkv']);
  });
});

describe('withDest', () => {
  const flagged = () => ({ ...row({ exists: true, dest: 'films/Y/Y.mkv' }), checked: true });

  it('oublie « déjà présent » quand la destination change : pas d’écrasement non signalé', () => {
    const r = { ...flagged(), ...withDest(flagged(), 'films/Z/Z.mkv') };
    expect(r.exists).toBe(false);
    expect(toRequests([r])[0].files[0]).toMatchObject({ dest: 'films/Z/Z.mkv', overwrite: false });
  });
  it('garde « déjà présent » pour la même destination', () => {
    expect(withDest(flagged(), 'films/Y/Y.mkv')).toEqual({ dest: 'films/Y/Y.mkv', exists: true });
  });
  it('applyCard oublie « déjà présent » si la destination change, pas si elle est saisie à la main', () => {
    expect(applyCard(flagged(), dunkirk)).toMatchObject({ dest: 'films/Dunkerque/Dunkerque.mkv', exists: false });
    expect(applyCard({ ...flagged(), destEdited: true }, dunkirk).exists).toBe(true);
  });
});

describe('pack', () => {
  const bb: TMDBResult = { id: 1396, title: 'Breaking Bad', year: 2008, animation: false, poster: '', tv: true };
  const pack = () => row({ name: 'Breaking.Bad.S03.rar', kind: 'series', tmdb: bb, season: 3, dest: 'series/BreakingBad/S03', pack: true });

  it('recompose vise le dossier de la saison du pack', () => {
    expect(recompose({ ...pack(), season: 5 })).toBe('series/BreakingBad/S05');
  });

  it('applyCard garde le type série et recalcule le dossier', () => {
    const anim: TMDBResult = { id: 2, title: 'Rick et Morty', year: 2013, animation: true, poster: '', tv: false };
    expect(applyCard(pack(), anim)).toMatchObject({ kind: 'series', dest: 'series/RickEtMorty/S03' });
  });

  it('toRequests envoie le dossier sans écrasement', () => {
    expect(toRequests([pack()])[0].files[0]).toEqual({
      input: 'https://h/x', name: 'Breaking.Bad.S03.rar', size: 100, dest: 'series/BreakingBad/S03', overwrite: false,
    });
  });
});
