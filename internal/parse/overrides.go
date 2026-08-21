package parse

import (
	"go/types"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

// collectOverrides emits OVERRIDES for methods that provide an implementation
// of a fully satisfied interface and for methods that shadow an embedded type.
// Go does not have an override keyword, so a same-name/same-signature method is
// not sufficient evidence by itself: the receiver must implement the complete
// interface method set.
func collectOverrides(c *Context) {
	interfaces := collectInterfaceMethods(c)

	for _, pkg := range c.Pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			if _, isInterface := tn.Type().Underlying().(*types.Interface); isInterface {
				continue
			}

			emitInterfaceOverrides(c, pkg.PkgPath, tn, interfaces)
			emitEmbeddedMethodOverrides(c, pkg.PkgPath, tn)
		}
	}
}

type interfaceMethods struct {
	iface   *types.Interface
	methods []imethod
}

type imethod struct {
	name  string
	fnID  string
	fn    *types.Func
	iface *types.Signature
}

func collectInterfaceMethods(c *Context) []interfaceMethods {
	var result []interfaceMethods
	for _, pkg := range c.Pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			iface, ok := tn.Type().Underlying().(*types.Interface)
			if !ok || iface.Empty() {
				continue
			}
			entry := interfaceMethods{iface: iface}
			for i := 0; i < iface.NumExplicitMethods(); i++ {
				method := iface.ExplicitMethod(i)
				sig, _ := method.Type().(*types.Signature)
				qualified := pkg.PkgPath + "." + tn.Name() + "." + method.Name()
				fnID := c.FuncByQName[qualified]
				if fnID == "" {
					fnID = ids.FunctionID(qualified)
				}
				entry.methods = append(entry.methods, imethod{
					name: method.Name(), fnID: fnID, fn: method, iface: sig,
				})
			}
			result = append(result, entry)
		}
	}
	return result
}

func emitInterfaceOverrides(c *Context, pkgPath string, tn *types.TypeName, interfaces []interfaceMethods) {
	value := tn.Type()
	pointer := types.NewPointer(value)
	for _, candidate := range interfaces {
		valueImplements := types.Implements(value, candidate.iface)
		pointerImplements := types.Implements(pointer, candidate.iface)
		if !valueImplements && !pointerImplements {
			continue
		}
		methodSet := types.NewMethodSet(value)
		if !valueImplements {
			methodSet = types.NewMethodSet(pointer)
		}
		for _, contract := range candidate.methods {
			selection := methodSet.Lookup(contract.fn.Pkg(), contract.name)
			if selection == nil {
				continue
			}
			implementation, ok := selection.Obj().(*types.Func)
			if !ok || !declaredOn(implementation, tn) {
				// Promoted methods belong to the embedded receiver. That receiver is
				// processed separately, so do not invent a method on the outer type.
				continue
			}
			implementationSig, _ := implementation.Type().(*types.Signature)
			if implementationSig == nil || contract.iface == nil || !signaturesCompatible(implementationSig, contract.iface) {
				continue
			}
			fromID := methodFuncID(c, pkgPath, tn.Name(), implementation)
			if fromID == "" || fromID == contract.fnID {
				continue
			}
			c.addRel(protocol.CodeRelationship{
				ID:               ids.RelationshipID(fromID, protocol.RelOverrides, contract.fnID),
				FromNodeID:       fromID,
				ToNodeID:         contract.fnID,
				RelationshipType: protocol.RelOverrides,
				Language:         "go",
				ProjectName:      c.projectName(),
				CallType:         "interface",
			})
		}
	}
}

func emitEmbeddedMethodOverrides(c *Context, pkgPath string, tn *types.TypeName) {
	st, ok := tn.Type().Underlying().(*types.Struct)
	if !ok {
		return
	}
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		if !field.Embedded() {
			continue
		}
		embedded := field.Type()
		named := namedOf(embedded)
		if named == nil || named.Obj() == nil || named.Obj().Pkg() == nil {
			continue
		}
		methodSet := types.NewMethodSet(embedded)
		for j := 0; j < methodSet.Len(); j++ {
			embeddedMethod, ok := methodSet.At(j).Obj().(*types.Func)
			if !ok {
				continue
			}
			declared := lookupDeclaredMethod(tn.Type(), embeddedMethod.Name())
			if declared == nil {
				continue
			}
			fromID := methodFuncID(c, pkgPath, tn.Name(), declared)
			toID := methodFuncID(c, named.Obj().Pkg().Path(), named.Obj().Name(), embeddedMethod)
			if fromID == "" || toID == "" || fromID == toID {
				continue
			}
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

func declaredOn(fn *types.Func, owner *types.TypeName) bool {
	signature, _ := fn.Type().(*types.Signature)
	return signature != nil && signature.Recv() != nil && namedOf(signature.Recv().Type()) != nil &&
		namedOf(signature.Recv().Type()).Obj() == owner
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

func lookupDeclaredMethod(t types.Type, name string) *types.Func {
	n, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	for i := 0; i < n.NumMethods(); i++ {
		method := n.Method(i)
		if method.Name() == name {
			return method
		}
	}
	return nil
}

func methodFuncID(c *Context, pkgPath, typeName string, fn *types.Func) string {
	qualified := pkgPath + "." + typeName + "." + fn.Name()
	if id := c.FuncByQName[qualified]; id != "" {
		return id
	}
	return ids.FunctionID(qualified)
}
