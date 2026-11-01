import { Routes } from '@angular/router';
import { MusicPage } from './music/music';
import { RunningPage } from './running/running';

export const routes: Routes = [
  { path: '', component: RunningPage },
  { path: 'music', component: MusicPage },
  { path: '**', redirectTo: '' },
];
