package parse

import (
	"go/types"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

// collectOverrides emits OVERRIDES:
//  1. method on T that implements interface method (same name + assignable signature)
//  2. method on T that shadows an embedded type's method of the same name
func collectOverrides(c *Context) {
	for _, pkg := range c.Pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		// index interface methods by name
		ifaceMethods := map[string][]imethod{}

		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			iface, ok := tn.Type().Underlying().(*types.Interface)
			if !ok {
				continue
			}
			for i := 0; i < iface.NumMethods(); i++ {
				m := iface.Method(i)
				sig, _ := m.Type().(*types.Signature)
				fnID := ids.FunctionID(tn.Pkg().Path() + "." + tn.Name() + "." + m.Name())
				if id, ok := c.FuncByQName[tn.Pkg().Path()+"."+tn.Name()+"."+m.Name()]; ok {
					fnID = id
				}
				ifaceMethods[m.Name()] = append(ifaceMethods[m.Name()], imethod{fnID: fnID, sig: sig})
			}
		}

		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			// methods of named type (value and pointer)
			ms := types.NewMethodSet(tn.Type())
			pms := types.NewMethodSet(types.NewPointer(tn.Type()))
			seen := map[string]bool{}
			addOverridesForMethodSet(c, pkg.PkgPath, tn, ms, ifaceMethods, seen)
			addOverridesForMethodSet(c, pkg.PkgPath, tn, pms, ifaceMethods, seen)

			// embed shadowing
			if st, ok := tn.Type().Underlying().(*types.Struct); ok {
				for i := 0; i < st.NumFields(); i++ {
					f := st.Field(i)
					if !f.Embedded() {
						continue
					}
					emb := f.Type()
					if p, ok := emb.(*types.Pointer); ok {
						emb = p.Elem()
					}
					named, ok := emb.(*types.Named)
					if !ok {
						continue
					}
					embMS := types.NewMethodSet(named)
					for j := 0; j < embMS.Len(); j++ {
						sel := embMS.At(j)
						embFn, _ := sel.Obj().(*types.Func)
						if embFn == nil {
							continue
						}
						// if T also declares same method name at depth 0
						if m := lookupDeclaredMethod(tn.Type(), embFn.Name()); m != nil {
							fromID := methodFuncID(c, pkg.PkgPath, tn.Name(), m)
							toID := methodFuncID(c, named.Obj().Pkg().Path(), named.Obj().Name(), embFn)
							if fromID != "" && toID != "" && fromID != toID {
								c.addRel(protocol.CodeRelationship{
									ID:               ids.RelationshipID(fromID, protocol.RelOverrides, toID),
									FromNodeID:       fromID,
									ToNodeID:         toID,
									RelationshipType: protocol.RelOverrides,
									Language:         "go",
									ProjectName:      c.projectName(),
									CallType:         "embed-shadow",
								})
							}
						}
					}
				}
			}
		}
	}
}

func addOverridesForMethodSet(
	c *Context,
	pkgPath string,
	tn *types.TypeName,
	ms *types.MethodSet,
	ifaceMethods map[string][]imethod,
	seen map[string]bool,
) {
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		// only methods declared on this type (not promoted) for interface satisfaction OVERRIDES
		if sel.Indirect() && len(sel.Index()) > 1 {
			// promoted via embed — skip for interface override edge from outer type's declared methods
		}
		fn, ok := sel.Obj().(*types.Func)
		if !ok {
			continue
		}
		// declared on this named type?
		recv := fn.Type().(*types.Signature).Recv()
		if recv == nil {
			continue
		}
		recvNamed := namedOf(recv.Type())
		if recvNamed == nil || recvNamed.Obj() != tn {
			// method not declared on tn itself
			if sel.Index()[0] != 0 || len(sel.Index()) != 1 {
				// might still be declared - check
				if !methodDeclaredOn(tn.Type(), fn.Name()) {
					continue
				}
			}
		}
		fromID := methodFuncID(c, pkgPath, tn.Name(), fn)
		if fromID == "" || seen[fromID] {
			continue
		}
		seen[fromID] = true
		fsig, _ := fn.Type().(*types.Signature)
		for _, cand := range ifaceMethods[fn.Name()] {
			if cand.sig == nil || fsig == nil {
				continue
			}
			if signaturesCompatible(fsig, cand.sig) {
				c.addRel(protocol.CodeRelationship{
					ID:               ids.RelationshipID(fromID, protocol.RelOverrides, cand.fnID),
					FromNodeID:       fromID,
					ToNodeID:         cand.fnID,
					RelationshipType: protocol.RelOverrides,
					Language:         "go",
					ProjectName:      c.projectName(),
					CallType:         "interface",
				})
			}
		}
	}
}

type imethod struct {
	fnID string
	sig  *types.Signature
}

func signaturesCompatible(impl, iface *types.Signature) bool {
	// Ignore receiver; compare params and results.
	if impl.Params().Len() != iface.Params().Len() {
		return false
	}
	for i := 0; i < impl.Params().Len(); i++ {
		if !types.Identical(impl.Params().At(i).Type(), iface.Params().At(i).Type()) {
			return false
		}
	}
	if impl.Results().Len() != iface.Results().Len() {
		return false
	}
	for i := 0; i < impl.Results().Len(); i++ {
		if !types.Identical(impl.Results().At(i).Type(), iface.Results().At(i).Type()) {
			return false
		}
	}
	return true
}

func namedOf(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := t.(*types.Named)
	return n
}

func methodDeclaredOn(t types.Type, name string) bool {
	return lookupDeclaredMethod(t, name) != nil
}

func lookupDeclaredMethod(t types.Type, name string) *types.Func {
	if n, ok := t.(*types.Named); ok {
		for i := 0; i < n.NumMethods(); i++ {
			m := n.Method(i)
			if m.Name() == name {
				return m
			}
		}
	}
	// also check pointer methods via MethodSet depth
	ms := types.NewMethodSet(t)
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		if len(sel.Index()) == 1 && sel.Obj().Name() == name {
			if fn, ok := sel.Obj().(*types.Func); ok {
				return fn
			}
		}
	}
	ms = types.NewMethodSet(types.NewPointer(t))
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		if len(sel.Index()) == 1 && sel.Obj().Name() == name {
			if fn, ok := sel.Obj().(*types.Func); ok {
				return fn
			}
		}
	}
	return nil
}

func methodFuncID(c *Context, pkgPath, typeName string, fn *types.Func) string {
	sigName := typeName + "." + fn.Name()
	q := pkgPath + "." + sigName
	if id, ok := c.FuncByQName[q]; ok {
		return id
	}
	// free-style id
	return ids.FunctionID(q)
}
