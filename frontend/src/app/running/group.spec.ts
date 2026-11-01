import { JobView, QueueFile, QueueView } from '../api/models';
import { activeFiles, fileLabel, fileMeta, summarize } from './group';
import { formatBytes } from '../format';

const file = (over: Partial<QueueFile>): QueueFile => ({
  id: 1, name: 'x.mkv', dest: 'series/TheBoys/S02/TheBoys.S02.E01.mkv', state: 'pending', error: '',
  bytes: 0, total: 0, speed: 0, attempts: 0, phase: '', host: '1fichier.com', ...over,
});

describe('summarize', () => {
  it('compte actifs, en attente, erreurs, reste et débit', () => {
    const job: JobView = { id: 1, kind: 'series', title: 'The Boys', files: [
      file({ id: 1, state: 'running', bytes: 400, total: 1000 }),
      file({ id: 2, state: 'error', bytes: 0, total: 500 }),
      file({ id: 3, state: 'pending', total: 200 }),
    ] };
    const view: QueueView = { paused: false, speed: 50, jobs: [job] };
    expect(summarize(view)).toEqual({ active: 1, pending: 1, errors: 1, bytesLeft: 800, speed: 50 });
  });
});

describe('activeFiles', () => {
  it('donne les fichiers actifs puis en attente, sans les terminés ni les erreurs', () => {
    const a: JobView = { id: 1, kind: 'series', title: 'A', files: [
      file({ id: 1, state: 'pending' }), file({ id: 2, state: 'done' }), file({ id: 3, state: 'running' }),
    ] };
    const b: JobView = { id: 2, kind: 'movie', title: 'B', files: [file({ id: 4, state: 'running' }), file({ id: 5, state: 'error' })] };
    expect(activeFiles([a, b]).map((r) => [r.job.id, r.file.id])).toEqual([[1, 3], [2, 4], [1, 1]]);
  });
});

describe('fileLabel', () => {
  const job = (kind: JobView['kind']): JobView => ({ id: 1, kind, title: 'x', files: [] });
  it('distingue épisode, pack, morceau et film', () => {
    expect(fileLabel(job('series'), file({}))).toBe('S02 E01');
    expect(fileLabel(job('series'), file({ dest: 'series/TheBoys/S02' }))).toBe('S02');
    expect(fileLabel(job('series'), file({ dest: 'series/TheBoys', name: 'The.Boys.s03.1080p.rar' }))).toBe('S03');
    expect(fileLabel(job('music'), file({ dest: 'musics/A/B/A - B - 01 - Titre.mp3' }))).toBe('A - B - 01 - Titre');
    expect(fileLabel(job('movie'), file({ dest: 'films/X/X.mkv' }))).toBe('');
  });
});

describe('fileMeta', () => {
  it('affiche la phase extraction sans durée restante', () => {
    expect(fileMeta(file({ state: 'running', phase: 'extraction', bytes: 45, total: 100, speed: 10 })))
      .toBe(`Extraction · 45 % · ${formatBytes(45)} / ${formatBytes(100)}`);
  });

  it('affiche le téléchargement avec sa vitesse et la durée restante', () => {
    expect(fileMeta(file({ state: 'running', bytes: 500, total: 1000, speed: 100 })))
      .toBe(`50 % · ${formatBytes(500)} / ${formatBytes(1000)} · ${formatBytes(100)}/s · 5 s`);
  });

  it('affiche un fichier en attente', () => {
    expect(fileMeta(file({ state: 'pending', total: 1000 }))).toBe(`en attente · ${formatBytes(1000)}`);
  });
});
