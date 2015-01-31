package zfs

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/random"
)

// REVIEW: this might deserve a whole separate package...?  much of this functionality is not zfs specific

type PersistenceTests struct {
}

var _ = Suite(&PersistenceTests{})

func (PersistenceTests) SetUpSuite(c *C) {
	// Skip all tests in this suite if not running as root.
	// Many zfs operations require root priviledges.
	skipIfNotRoot(c)
}

func (s *PersistenceTests) SetUpTest(c *C) {
}

func (s *PersistenceTests) TestApplePie(c *C) {
	stringOfDreams := random.String(12)
	vmanDBfilePath := fmt.Sprintf("/tmp/flynn-volumes-%s.bold", stringOfDreams)

	// new volume manager with a new backing zfs vdev file and a new boltdb
	volProv, err := NewProvider(&ProviderConfig{
		DatasetName: fmt.Sprintf("flynn-test-dataset-%s", stringOfDreams),
		Make: &MakeDev{
			BackingFilename: fmt.Sprintf("/tmp/flynn-test-zpool-%s.vdev", stringOfDreams),
			Size:            int64(math.Pow(2, float64(30))),
		},
	})
	c.Assert(err, IsNil)

	// new volume manager with that shiney new backing zfs vdev file and a new boltdb
	vman, err := volume.NewManager(
		vmanDBfilePath,
		func() (volume.Provider, error) { return volProv, nil },
	)
	c.Assert(err, IsNil)

	// make a volume
	vol1, err := vman.NewVolume()
	c.Assert(err, IsNil)

	// make a named volume
	vol2, err := vman.CreateOrGetNamedVolume("aname", "")
	c.Assert(err, IsNil)

	// assert existence of filesystems; emplace some data
	f, err := os.Create(filepath.Join(vol1.(*zfsVolume).basemount, "alpha"))
	c.Assert(err, IsNil)
	f.Close()
	f, err = os.Create(filepath.Join(vol2.(*zfsVolume).basemount, "beta"))
	c.Assert(err, IsNil)
	f.Close()

	// close persistence
	c.Assert(vman.PersistenceDBClose(), IsNil)

	// ?  hack zfs export/umounting to emulate host shutdown

	// restore
	vman, err := volume.NewManager(
		vmanDBfilePath,
		func() (volume.Provider, error) {
			c.Fatal("default provider setup should not be called if the previous provider was restored")
		},
	)
	c.Assert(err, IsNil)

	// assert volumes

	// assert named volumes

	// assert existences of filesystems and previous data
	c.Assert(vol1.(*zfsVolume).basemount, DirContains, []string{"alpha"})
	c.Assert(vol2.(*zfsVolume).basemount, DirContains, []string{"beta"})

}
