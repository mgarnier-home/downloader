import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { App } from './app';
import { routes } from './app.routes';

describe('App', () => {
  it('affiche les pastilles et le compteur', async () => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideRouter(routes)],
    });
    const fixture = TestBed.createComponent(App);
    const http = TestBed.inject(HttpTestingController);
    TestBed.tick(); // lance les requêtes des ressources
    http.expectOne('/api/status').flush({
      alldebrid: { ok: true, premium: true, until: '2027-03-05', error: '' },
      deezer: { ok: true, error: '' },
      freeBytes: 4_100_000_000_000,
    });
    http.expectOne('/api/queue').flush({
      paused: false,
      speed: 0,
      jobs: [{ id: 1, kind: 'movie', title: 'x', files: [
        { id: 1, state: 'running' }, { id: 2, state: 'error' }, { id: 3, state: 'pending' },
      ] }],
    });
    await fixture.whenStable();
    fixture.detectChanges();
    const text = (fixture.nativeElement as HTMLElement).textContent ?? '';
    expect(text).toContain('premium jusqu\'au 5 mars 2027');
    expect(text).toContain('4,1 To libres');
    expect(fixture.nativeElement.querySelector('.tabs em').textContent).toBe('2');
  });
});
