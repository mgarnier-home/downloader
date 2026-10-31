import { Routes } from '@angular/router';
import { HistoryPage } from './pages/history';
import { MusicPage } from './pages/music';
import { RunningPage } from './pages/running';
import { VideosPage } from './pages/videos';

export const routes: Routes = [
  { path: '', component: RunningPage },
  { path: 'videos', component: VideosPage },
  { path: 'music', component: MusicPage },
  { path: 'history', component: HistoryPage },
  { path: '**', redirectTo: '' },
];
