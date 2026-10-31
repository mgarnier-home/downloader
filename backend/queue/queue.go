// Package queue exécute les téléchargements enregistrés dans la base, avec reprise,
// nouvelles tentatives et rangement des fichiers terminés.
package queue

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/mgarnier-home/utils/logger"

	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/store"
)

// Progress est l'avancement d'un transfert en cours ; Speed en octets/s, moyenne sur 5 s.
type Progress struct {
	Bytes, Total int64
	Speed        float64
}

const (
	speedWindow = 5 * time.Second
	rescanEvery = 2 * time.Second
)

// Manager planifie les fichiers en attente : VideoLimit transferts pour les séries, films et
// animés réunis, MusicLimit pour la musique.
type Manager struct {
	Store                  *store.Store
	Sources                []source.Source
	MediaDir               string
	VideoLimit, MusicLimit int
	Delays                 []time.Duration                            // attente avant chaque nouvelle tentative
	Sleep                  func(context.Context, time.Duration) error // remplaçable en test
	Finish                 func(f store.File, path string) error      // appelé sur le .part, avant le déplacement

	mu      sync.Mutex
	wake    chan struct{}
	paused  bool
	running map[int64]*transfer
	wg      sync.WaitGroup
}

type sample struct {
	at    time.Time
	bytes int64
}

// transfer est un téléchargement en cours ; ses champs sont protégés par Manager.mu.
type transfer struct {
	cancel  context.CancelFunc
	done    chan struct{}
	music   bool
	removed bool // interrompu par Cancel : l'état en base ne change pas
	bytes   int64
	total   int64
	samples []sample
}

func (t *transfer) reset(bytes, total int64, now time.Time) {
	t.bytes, t.total, t.samples = bytes, total, []sample{{now, bytes}}
}

func (t *transfer) add(n int, now time.Time) {
	t.bytes += int64(n)
	if len(t.samples) == 0 || now.Sub(t.samples[len(t.samples)-1].at) >= 100*time.Millisecond {
		t.samples = append(t.samples, sample{now, t.bytes})
	}
}

// speed renvoie le débit moyen depuis le plus ancien point de moins de 5 s.
func (t *transfer) speed(now time.Time) float64 {
	for len(t.samples) > 0 && now.Sub(t.samples[0].at) > speedWindow {
		t.samples = t.samples[1:]
	}
	if len(t.samples) == 0 {
		return 0
	}
	dt := now.Sub(t.samples[0].at).Seconds()
	if dt <= 0 {
		return 0
	}
	return float64(t.bytes-t.samples[0].bytes) / dt
}

// init prépare les champs internes ; appelé sous m.mu.
func (m *Manager) init() {
	if m.wake == nil {
		m.wake = make(chan struct{}, 1)
		m.running = map[int64]*transfer{}
	}
}

// Run remet en attente les fichiers restés « en cours », puis planifie jusqu'à l'annulation de ctx.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.init()
	wake := m.wake
	m.mu.Unlock()
	if err := m.Store.ResetRunning(); err != nil {
		logger.Errorf("file : remise en attente impossible : %v", err)
	}
	tick := time.NewTicker(rescanEvery)
	defer tick.Stop()
	for {
		m.schedule(ctx)
		select {
		case <-ctx.Done():
			m.wg.Wait()
			return
		case <-wake:
		case <-tick.C:
		}
	}
}

// Wake signale un nouveau travail ; ne bloque jamais.
func (m *Manager) Wake() {
	m.mu.Lock()
	m.init()
	wake := m.wake
	m.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

// Pause interrompt les transferts en cours (le .part reste) et bloque les démarrages.
func (m *Manager) Pause() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = true
	for _, t := range m.running {
		t.cancel()
	}
	logger.Infof("file : pause")
}

func (m *Manager) Resume() {
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()
	logger.Infof("file : reprise")
	m.Wake()
}

func (m *Manager) Paused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}

// Cancel interrompt le transfert de fileID, attend sa fin et supprime son .part. L'appelant
// supprime ou remplace ensuite le fichier en base.
// shortcut: un fichier en attente peut démarrer entre Cancel et sa suppression en base ; fenêtre
// de quelques microsecondes, à fermer par un verrou partagé avec l'API si cela se produit.
func (m *Manager) Cancel(fileID int64) {
	m.mu.Lock()
	t := m.running[fileID]
	if t != nil {
		t.removed = true
		t.cancel()
	}
	m.mu.Unlock()
	if t != nil {
		<-t.done
	}
	if err := os.Remove(m.partPath(fileID)); err != nil && !os.IsNotExist(err) {
		logger.Warnf("fichier %d : suppression du .part impossible : %v", fileID, err)
	}
}

func (m *Manager) Progress() map[int64]Progress {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	out := make(map[int64]Progress, len(m.running))
	for id, t := range m.running {
		out[id] = Progress{Bytes: t.bytes, Total: t.total, Speed: t.speed(now)}
	}
	return out
}

// schedule démarre les fichiers en attente dans la limite des places libres.
func (m *Manager) schedule(ctx context.Context) {
	files, err := m.Store.Files(store.StatePending)
	if err != nil {
		logger.Errorf("file : lecture des fichiers en attente : %v", err)
		return
	}
	ids := make([]int64, len(files))
	for i, f := range files {
		ids[i] = f.JobID
	}
	jobs, err := m.Store.Jobs(ids)
	if err != nil {
		logger.Errorf("file : lecture des jobs : %v", err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	if m.paused {
		return
	}
	for _, f := range files {
		music := jobs[f.JobID].Kind == naming.Music
		if m.running[f.ID] == nil && m.busy(music) < m.limit(music) {
			m.start(ctx, f, music)
		}
	}
}

func (m *Manager) limit(music bool) int {
	if music {
		return m.MusicLimit
	}
	return m.VideoLimit
}

// busy compte les transferts en cours de la même famille ; appelé sous m.mu.
func (m *Manager) busy(music bool) int {
	n := 0
	for _, t := range m.running {
		if t.music == music {
			n++
		}
	}
	return n
}

// start passe f en cours et lance son transfert ; appelé sous m.mu.
func (m *Manager) start(ctx context.Context, f store.File, music bool) {
	if err := m.Store.SetState(f.ID, store.StateRunning, ""); err != nil {
		logger.Errorf("fichier %d : passage en cours impossible : %v", f.ID, err)
		return
	}
	tctx, cancel := context.WithCancel(ctx)
	t := &transfer{cancel: cancel, done: make(chan struct{}), music: music}
	t.reset(0, f.Size, time.Now())
	m.running[f.ID] = t
	m.wg.Add(1)
	go m.work(tctx, f, t)
}
