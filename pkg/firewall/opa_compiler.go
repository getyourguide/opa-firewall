package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/cainelli/opa-firewall/pkg/iptree"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage/inmem"
)

func (firewall *Firewall) periodicallyCompile() {
	for {
		select {
		case <-time.After(10 * time.Second):
			firewall.Logger.Info("recompiling rules")
			firewall.Compile()
		}
	}
}

// Compile ...
// TODO:
//   * build IP tree
//   * gracefully handle errors
func (firewall *Firewall) Compile() {
	regoFunctions := []func(r *rego.Rego){
		rego.Query(fmt.Sprintf("data")),
		firewall.registerCustomBultin(),
	}
	stores := make(map[string]interface{})
	ipTrees := make(IPTrees)

	// TODO: implement mutex to avoid race conditions
	for _, policy := range firewall.Policies {
		// test module before adding it to the map.
		if err := testRego(policy.Rego); err != nil {
			// TODO: fix testRego and skip on error.
			// continue
		}

		// test if data is json compatible.
		_, err := json.Marshal(policy.Data)
		if err != nil {
			// log error
			continue
		} else if policy.Data != nil {
			stores[policy.Name] = policy.Data
		}

		if policy.IPBuckets != nil {
			for bucketName, bucket := range policy.IPBuckets {
				ipTree := iptree.New()
				for ipString, expireAt := range bucket {
					ip := net.ParseIP(ipString)
					err := ipTree.AddIP(ip, expireAt)
					if err != nil {
						firewall.Logger.Error(err)
						continue
					}

					firewall.Logger.Infof("bucket: %s ip: %v, expireAt: %v", bucketName, ip, expireAt)
				}
			}

		}

		regoFunctions = append(regoFunctions, rego.Package(policy.Name))
		regoFunctions = append(regoFunctions, rego.Module(policy.Name, policy.Rego))
	}

	dataJSON, err := json.Marshal(stores)
	if err != nil {
		firewall.Logger.Error(err)
		return
	}

	store := inmem.NewFromReader(bytes.NewBuffer(dataJSON))
	regoFunctions = append(regoFunctions, rego.Store(store))

	regoFunctions = append(regoFunctions)
	r := rego.New(
		regoFunctions...,
	)

	preparedEval, err := r.PrepareForEval(firewall.context)
	if err != nil {
		firewall.Logger.Error(err)
		return
	}

	firewall.PreparedEval = preparedEval
	firewall.IPTrees = ipTrees
}
