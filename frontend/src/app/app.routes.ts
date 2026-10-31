import { Routes } from '@angular/router';
import { HistoryPage } from './pages/history';
import { MusicPage } from './pages/music';
import { RunningPage } from './running/running';
import { VideosPage } from './videos/videos';

export const routes: Routes = [
  { path: '', component: RunningPage },
  { path: 'videos', component: VideosPage },
  { path: 'music', component: MusicPage },
  { path: 'history', component: HistoryPage },
  { path: '**', redirectTo: '' },
];
