import { JobView, QueueFile, QueueView } from '../api/models';
import { episodeCells, summarize } from './group';

const file = (over: Partial<QueueFile>): QueueFile => ({
  id: 1, name: 'x.mkv', dest: 'series/TheBoys/S02/TheBoys.S02.E01.mkv', state: 'pending', error: '',
  bytes: 0, total: 0, speed: 0, attempts: 0, ...over,
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

describe('episodeCells', () => {
  it('donne une cellule par fichier, dans l’ordre, avec le pourcentage des actifs', () => {
    const job: JobView = { id: 1, kind: 'series', title: 'The Boys', files: [
      file({ id: 1, dest: 'series/TheBoys/S02/TheBoys.S02.E01.mkv', state: 'done' }),
      file({ id: 2, dest: 'series/TheBoys/S02/TheBoys.S02.E02.mkv', state: 'error' }),
      file({ id: 3, dest: 'series/TheBoys/S02/TheBoys.S02.E03.mkv', state: 'running', bytes: 620, total: 1000 }),
      file({ id: 4, dest: 'series/TheBoys/S02/TheBoys.S02.E04.mkv', state: 'pending' }),
    ] };
    expect(episodeCells(job)).toEqual([
      { label: 'E01', state: 'done' },
      { label: 'E02', state: 'error' },
      { label: 'E03', state: 'running', percent: 62 },
      { label: 'E04', state: 'pending' },
    ]);
  });
});
