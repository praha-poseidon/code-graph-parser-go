package parse

import (
	"go/types"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

// collectImplements emits GO_SATISFIES: concrete named type → interface it fully satisfies.
func collectImplements(c *Context) {
	// gather all interface named types in loaded packages
	type named struct {
		pkgPath string
		name    string
		tn      *types.TypeName
		iface   *types.Interface
	}
	var ifaces []named
	var concretes []named

	for _, pkg := range c.Pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok || tn.Type() == nil {
				continue
			}
			switch u := tn.Type().Underlying().(type) {
			case *types.Interface:
				if u.Empty() {
					continue
				}
				ifaces = append(ifaces, named{pkg.PkgPath, tn.Name(), tn, u})
			default:
				// skip interfaces only
				if _, ok := tn.Type().Underlying().(*types.Interface); !ok {
					concretes = append(concretes, named{pkg.PkgPath, tn.Name(), tn, nil})
				}
			}
		}
	}

	for _, conc := range concretes {
		fromQ := conc.pkgPath + "." + conc.name
		fromID, ok := c.UnitByQName[fromQ]
		if !ok {
			continue
		}
		// pointer and value receivers both: check *T and T
		t := conc.tn.Type()
		ptr := types.NewPointer(t)
		for _, iface := range ifaces {
			toQ := iface.pkgPath + "." + iface.name
			toID, ok := c.UnitByQName[toQ]
			if !ok {
				toID = ids.UnitID(toQ)
			}
			if toID == fromID {
				continue
			}
			if types.Implements(t, iface.iface) || types.Implements(ptr, iface.iface) {
				c.addRel(protocol.CodeRelationship{
					ID:               ids.RelationshipID(fromID, protocol.RelSatisfies, toID),
					FromNodeID:       fromID,
					ToNodeID:         toID,
					RelationshipType: protocol.RelSatisfies,
					Language:         "go",
					ProjectName:      c.projectName(),
				})
			}
		}
	}
}
