import { Component, computed, inject, signal } from '@angular/core';
import { ApiService } from '../api/api.service';
import { Kind, TMDBResult } from '../api/models';
import { apiError, formatBytes, KIND_CLASS, KIND_LABEL } from '../format';
import { applyCard, EditableRow, recompose, toEditable, toRequests, withDest, workName } from './rows';

interface Picker { key: string; q: string; tv: boolean; results: TMDBResult[]; busy: boolean; error: string }
interface Group { key: string; title: string; idx: number[]; visible: number[]; hidden: number }

const KINDS: { value: Kind; label: string }[] = [
  { value: 'series', label: 'Série' }, { value: 'movie', label: 'Film' }, { value: 'anime', label: 'Animé' },
];
const guess = (name: string) => name.replace(/\.[^.]+$/, '').replace(/[._]/g, ' ');

@Component({
  selector: 'app-videos',
  templateUrl: './videos.html',
  styleUrl: './videos.css',
})
export class VideosPage {
  private readonly api = inject(ApiService);
  protected readonly kinds = KINDS;
  protected readonly kindClass = KIND_CLASS;
  protected readonly kindLabel = KIND_LABEL;
  protected readonly formatBytes = formatBytes;

  protected readonly links = signal('');
  protected readonly rows = signal<EditableRow[]>([]);
  protected readonly busy = signal(false);
  protected readonly launching = signal(false);
  protected readonly error = signal('');
  protected readonly expanded = signal<Set<string>>(new Set());
  protected readonly editing = signal(-1);
  protected readonly picker = signal<Picker | null>(null);

  private static key(r: EditableRow, i: number): string {
    if (!r.online || r.kind === '') return `x${i}`;
    return r.tmdb ? `t${r.tmdb.id}` : `n${workName(r.dest)}|${r.kind}`;
  }

  /** Lignes regroupées par œuvre ; au-delà de deux lignes d'une série, le reste est replié. */
  protected readonly groups = computed<Group[]>(() => {
    const map = new Map<string, Group>();
    this.rows().forEach((r, i) => {
      const key = VideosPage.key(r, i);
      const g = map.get(key) ?? { key, title: r.tmdb?.title ?? workName(r.dest), idx: [], visible: [], hidden: 0 };
      g.idx.push(i);
      map.set(key, g);
    });
    return [...map.values()].map((g) => {
      const fold = this.rows()[g.idx[0]].kind === 'series' && g.idx.length > 2 && !this.expanded().has(g.key);
      return { ...g, visible: fold ? g.idx.slice(0, 2) : g.idx, hidden: fold ? g.idx.length - 2 : 0 };
    });
  });
  protected readonly count = computed(() => toRequests(this.rows()).reduce((n, r) => n + r.files.length, 0));
  protected readonly summary = computed(() => {
    const rows = this.rows();
    const found = new Set(rows.filter((r) => r.tmdb).map((r) => r.tmdb!.id)).size;
    return `${rows.filter((r) => r.online).length} liens en ligne sur ${rows.length} · ${found} fiches TMDB trouvées`;
  });

  protected patch(i: number, change: Partial<EditableRow>) {
    this.rows.update((rs) => rs.map((r, j) => (j === i ? { ...r, ...change, message: '' } : r)));
  }

  protected async analyze() {
    const links = this.links().split(/\s+/).filter(Boolean);
    if (!links.length) return;
    this.busy.set(true);
    this.error.set('');
    try {
      this.rows.set((await this.api.analyze(links)).map(toEditable));
      this.expanded.set(new Set());
      this.picker.set(null);
    } catch (e) {
      this.error.set(apiError(e, "L'analyse a échoué."));
    }
    this.busy.set(false);
  }

  /** Après un changement de type, de numéro ou de bonus, la destination est recalculée. */
  protected retype(i: number, change: Partial<EditableRow>) {
    const r = { ...this.rows()[i], ...change };
    const dest = r.kind === '' ? r.dest : recompose(r);
    this.patch(i, { ...change, ...withDest(r, dest), checked: r.checked || (r.online && !r.exists && !r.dest && dest !== '') });
  }

  protected setKind(i: number, v: string) { this.retype(i, { kind: v as Kind }); }
  protected setNumber(i: number, field: 'season' | 'episode', v: string) { this.retype(i, { [field]: Math.max(0, Number(v) || 0) }); }

  protected edit(i: number) {
    this.editing.set(i);
    setTimeout(() => document.getElementById('dest-' + i)?.focus());
  }
  protected setDest(i: number, dest: string) {
    if (this.editing() !== i) return; // Échap a déjà fermé l'éditeur : le blur ne sauvegarde pas
    this.editing.set(-1);
    const r = this.rows()[i];
    this.patch(i, { ...withDest(r, dest.trim()), destEdited: true, checked: r.checked || (r.online && !r.exists && !r.dest && dest.trim() !== '') });
  }

  protected toggle(key: string) {
    this.expanded.update((s) => {
      const n = new Set(s);
      if (!n.delete(key)) n.add(key);
      return n;
    });
  }

  protected openPicker(g: Group) {
    if (this.picker()?.key === g.key) return this.picker.set(null);
    const r = this.rows()[g.idx[0]];
    this.picker.set({ key: g.key, q: r.tmdb?.title ?? guess(r.name), tv: r.tmdb?.tv ?? r.kind === 'series', results: [], busy: false, error: '' });
    return this.search();
  }
  protected setQuery(q: string) { this.picker.update((p) => p && { ...p, q }); }
  protected async search() {
    const p = this.picker();
    if (!p || !p.q.trim()) return;
    this.picker.set({ ...p, busy: true, error: '' });
    try {
      const results = await this.api.tmdbSearch(p.q.trim(), p.tv);
      this.picker.update((c) => c && { ...c, results, busy: false, error: results.length ? '' : 'Aucun résultat.' });
    } catch (e) {
      this.picker.update((c) => c && { ...c, busy: false, error: apiError(e, 'La recherche TMDB a échoué.') });
    }
  }
  /** La fiche choisie vaut pour toutes les lignes de l'œuvre ; la destination reste à relire. */
  protected choose(g: Group, tmdb: TMDBResult) {
    this.rows.update((rs) => rs.map((r, i) => (g.idx.includes(i) ? applyCard(r, tmdb) : r)));
    this.picker.set(null);
  }

  protected async launch() {
    this.launching.set(true);
    let failed = false;
    for (const req of toRequests(this.rows())) {
      const inputs = new Set(req.files.map((f) => f.input));
      try {
        await this.api.enqueue(req);
        this.rows.update((rs) => rs.filter((r) => !(r.checked && inputs.has(r.input))));
      } catch (e) {
        failed = true;
        const message = apiError(e, "L'ajout à la file a échoué.");
        this.rows.update((rs) => rs.map((r) => (r.checked && inputs.has(r.input) ? { ...r, message } : r)));
      }
    }
    this.launching.set(false);
    if (failed) return;
    this.links.set('');
    this.api.queue().reload();
  }
}
