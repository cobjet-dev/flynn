package volume

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/boltdb/bolt"
)

/*
	volume.Manager providers interfaces for both provisioning volume backends, and then creating volumes using them.

	There is one volume.Manager per host daemon process.
*/
type Manager struct {
	mutex sync.Mutex

	// `map[providerName]provider`
	//
	// It's possible to configure multiple volume providers for a flynn-host daemon.
	// This can be used to create volumes using providers backed by different storage resources,
	// or different volume backends entirely.
	providers map[string]Provider

	// `map[volume.Id]volume`
	volumes map[string]Volume

	// `map[wellKnownName]volume.Id`
	namedVolumes map[string]string

	stateDB *bolt.DB
}

var NoSuchProvider = errors.New("no such provider")
var ProviderAlreadyExists = errors.New("that provider id already exists")

func NewManager(stateFilePath string, defProvFn func() (Provider, error)) (*Manager, error) {
	stateDB, err := initializePersistence(stateFilePath)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		providers:    map[string]Provider{},
		volumes:      map[string]Volume{},
		namedVolumes: map[string]string{},
		stateDB:      stateDB,
	}
	if err := m.restore(); err != nil {
		return nil, err
	}
	if _, ok := m.providers["default"]; !ok {
		p, err := defProvFn()
		if err != nil {
			return nil, fmt.Errorf("could not initialize default provider: %s", err)
		}
		m.providers["default"] = p
	}
	return m, nil
}

func (m *Manager) AddProvider(id string, p Provider) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.providers[id]; ok {
		return ProviderAlreadyExists
	}
	m.providers[id] = p
	m.persist(func(tx *bolt.Tx) error { return m.persistProvider(tx, id) })
	return nil
}

func (m *Manager) Volumes() map[string]Volume {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	r := make(map[string]Volume)
	for k, v := range m.volumes {
		r[k] = v
	}
	return r
}

/*
	volume.Manager implements the volume.Provider interface by
	delegating NewVolume requests to the default Provider.
*/
func (m *Manager) NewVolume() (Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.newVolumeFromProviderLocked("")
}

/*
	volume.Manager implements the volume.Provider interface by
	delegating NewVolume requests to the named Provider.
*/
func (m *Manager) NewVolumeFromProvider(providerID string) (Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.newVolumeFromProviderLocked(providerID)
}

func (m *Manager) newVolumeFromProviderLocked(providerID string) (Volume, error) {
	if providerID == "" {
		providerID = "default"
	}
	if p, ok := m.providers[providerID]; ok {
		return managerProviderProxy{p, m}.NewVolume()
	} else {
		return nil, NoSuchProvider
	}
}

func (m *Manager) GetVolume(id string) Volume {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.volumes[id]
}

func initializePersistence(stateFilePath string) (*bolt.DB, error) {
	if stateFilePath == "" {
		return nil, nil
	}
	// open/initialize db
	if err := os.MkdirAll(filepath.Dir(stateFilePath), 0755); err != nil {
		panic(fmt.Errorf("could not mkdir for volume persistence db: %s", err))
	}
	stateDB, err := bolt.Open(stateFilePath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		panic(fmt.Errorf("could not open volume persistence db: %s", err))
	}
	if err := stateDB.Update(func(tx *bolt.Tx) error {
		// idempotently create buckets.  (errors ignored because they're all compile-time impossible args checks.)
		tx.CreateBucketIfNotExists([]byte("volumes"))
		tx.CreateBucketIfNotExists([]byte("providers"))
		tx.CreateBucketIfNotExists([]byte("namedVolumes"))
		return nil
	}); err != nil {
		return nil, fmt.Errorf("could not initialize volume persistence db: %s", err)
	}
	return stateDB, nil
}

func (m *Manager) restore() error {
	if err := m.stateDB.View(func(tx *bolt.Tx) error {
		//volumesBucket := tx.Bucket([]byte("volumes"))
		//providersBucket := tx.Bucket([]byte("providers"))
		//namedVolumesBucket := tx.Bucket([]byte("namedVolumes"))

		// TODO finish serialize first
		return nil
	}); err != nil && err != io.EOF {
		return fmt.Errorf("could not restore from volume persistence db: %s", err)
	}
	return nil
}

func (m *Manager) persist(fn func(*bolt.Tx) error) {
	// maintenance note: db update calls should generally immediately follow
	// the matching in-memory map updates, *and be under the same mutex*.
	if m.stateDB == nil {
		return
	}
	if err := m.stateDB.Update(func(tx *bolt.Tx) error {
		return fn(tx)
	}); err != nil {
		panic(fmt.Errorf("could not commit volume persistence update: %s", err))
	}
}

/*
	Close the DB that persists the volume state.
	This is not called in typical flow because there's no need to release this file descriptor,
	but it is needed in testing so that bolt releases locks such that the file can be reopened.
*/
func (m *Manager) PersistenceDBClose() error {
	return m.stateDB.Close()
}

// Called to sync changes to disk when a VolumeInfo is updated
func (m *Manager) persistVolume(tx *bolt.Tx, id string) error {
	// Save the general volume info
	volumesBucket := tx.Bucket([]byte("volumes"))
	k := []byte(id)
	vol, ok := m.volumes[id]
	if !ok {
		volumesBucket.Delete(k)
	} else {
		b, err := json.Marshal(vol)
		if err != nil {
			return fmt.Errorf("failed to serialize volume info: %s", err)
		}
		err = volumesBucket.Put(k, b)
		if err != nil {
			return fmt.Errorf("could not persist volume info to boltdb: %s", err)
		}
	}
	// Save any provider-specific metadata associated with the volume
	providerAttachmentBucket, err := tx.Bucket([]byte("providers")).CreateBucketIfNotExists([]byte("volumes"))
	if err != nil {
		return fmt.Errorf("could not persist volume info to boltdb: %s", err)
	}
	if !ok {
		providerAttachmentBucket.Delete(k)
	} else {
		// FIXME: the Volume interface currently doesn't return a link to its own provider
		// unclear if we should just do that, or if there's going to be a deeper refactor in order between that and volume.Info
		//b, err = vol.Provider().MarshalVolumeState(id)
		//if err != nil {
		//	return fmt.Errorf("failed to serialize volume info: %s", err)
		//}
		//err = providerAttachmentBucket.Put(k, b)
		//if err != nil {
		//	return fmt.Errorf("could not persist volume info to boltdb: %s", err)
		//}
	}
	return nil
}

func (m *Manager) persistProvider(tx *bolt.Tx, id string) error {
	//providersBucket := tx.Bucket([]byte("providers"))
	// TODO
	// This method does *not* include re-serializing per-volume state, because we assume that
	// hasn't changed unless the change request for the volume came through us already.
	return nil
}

func (m *Manager) persistNamedVolume(tx *bolt.Tx, name string) error {
	namedVolumesBucket := tx.Bucket([]byte("namedVolumes"))
	k := []byte(name)
	volID, ok := m.namedVolumes[name]
	if !ok {
		namedVolumesBucket.Delete(k)
	} else {
		err := namedVolumesBucket.Put(k, []byte(volID))
		if err != nil {
			return fmt.Errorf("could not persist namedVolume info to boltdb: %s", err)
		}
	}
	return nil
}

/*
	Proxies `volume.Provider` while making sure the manager remains
	apprised of all volume lifecycle events.
*/
type managerProviderProxy struct {
	Provider
	m *Manager
}

func (p managerProviderProxy) NewVolume() (Volume, error) {
	v, err := p.Provider.NewVolume()
	if err != nil {
		return v, err
	}
	p.m.volumes[v.Info().ID] = v
	p.m.persist(func(tx *bolt.Tx) error { return p.m.persistVolume(tx, v.Info().ID) })
	return v, err
}

/*
	Gets a reference to a volume by name if that exists; if no volume is so named,
	it is created using the named provider (the zero string can be used to invoke
	the default provider).
*/
func (m *Manager) CreateOrGetNamedVolume(name string, providerID string) (Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if v, ok := m.namedVolumes[name]; ok {
		return m.volumes[v], nil
	}
	v, err := m.newVolumeFromProviderLocked(providerID)
	if err != nil {
		return nil, err
	}
	m.namedVolumes[name] = v.Info().ID
	m.persist(func(tx *bolt.Tx) error { return m.persistNamedVolume(tx, name) })
	return v, nil
}
