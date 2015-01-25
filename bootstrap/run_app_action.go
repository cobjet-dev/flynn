package bootstrap

import (
	"errors"
	"net/url"
	"sort"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/resource"
	"github.com/flynn/flynn/pkg/schedutil"
)

type RunAppAction struct {
	*ct.ExpandedFormation

	ID        string         `json:"id"`
	AppStep   string         `json:"app_step"`
	Resources []*ct.Provider `json:"resources,omitempty"`
}

type Provider struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func init() {
	Register("run-app", &RunAppAction{})
}

type RunAppState struct {
	*ct.ExpandedFormation
	Providers []*ct.Provider       `json:"providers"`
	Resources []*resource.Resource `json:"resources"`
	Jobs      []Job                `json:"jobs"`
}

type Job struct {
	HostID string `json:"host_id"`
	JobID  string `json:"job_id"`
}

func (a *RunAppAction) Run(s *State) error {
	if a.AppStep != "" {
		data, err := getAppStep(s, a.AppStep)
		if err != nil {
			return err
		}
		a.App = data.App
		procs := a.Processes
		a.ExpandedFormation = data.ExpandedFormation
		a.Processes = procs
	}
	as := &RunAppState{
		ExpandedFormation: a.ExpandedFormation,
		Resources:         make([]*resource.Resource, 0, len(a.Resources)),
		Providers:         make([]*ct.Provider, 0, len(a.Resources)),
	}
	s.StepData[a.ID] = as

	if a.App == nil {
		a.App = &ct.App{}
	}
	if a.App.ID == "" {
		a.App.ID = random.UUID()
	}
	if a.Artifact == nil {
		return errors.New("bootstrap: artifact must be set")
	}
	if a.Artifact.ID == "" {
		a.Artifact.ID = random.UUID()
	}
	if a.Release == nil {
		return errors.New("bootstrap: release must be set")
	}
	if a.Release.ID == "" {
		a.Release.ID = random.UUID()
	}
	a.Release.ArtifactID = a.Artifact.ID
	if a.Release.Env == nil {
		a.Release.Env = make(map[string]string)
	}
	interpolateRelease(s, a.Release)

	for _, p := range a.Resources {
		u, err := url.Parse(p.URL)
		if err != nil {
			return err
		}
		lookupDiscoverdURLHost(u, time.Second)
		res, err := resource.Provision(u.String(), nil)
		if err != nil {
			return err
		}
		as.Providers = append(as.Providers, p)
		as.Resources = append(as.Resources, res)
		for k, v := range res.Env {
			a.Release.Env[k] = v
		}
	}

	cc, err := s.ClusterClient()
	if err != nil {
		return err
	}
	for typ, count := range a.Processes {
		hosts, err := cc.ListHosts()
		if err != nil {
			return err
		}
		sort.Sort(schedutil.HostSlice(hosts))
		for i := 0; i < count; i++ {
			job, err := startJob(s, hosts[i%len(hosts)].ID, utils.JobConfig(a.ExpandedFormation, typ))
			if err != nil {
				return err
			}
			as.Jobs = append(as.Jobs, *job)
		}
	}

	return nil
}
