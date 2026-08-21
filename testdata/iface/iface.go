package iface

import "example.com/iface/contract"

type Greeter interface {
	Greet() string
}

type Person struct {
	Name string
}

func (p Person) Greet() string {
	return "hi " + p.Name
}

type Shouter interface {
	Greeter
	Shout() string
}

type LoudPerson struct {
	Person
}

func (l LoudPerson) Shout() string {
	return "HEY " + l.Name
}

type Partial interface {
	First()
	Second()
}

type Almost struct{}

func (Almost) First() {}

type PointerGreeter interface {
	PointerGreet()
}

type PointerPerson struct{}

func (*PointerPerson) PointerGreet() {}

type EmbeddedBase struct{}

func (EmbeddedBase) Ping() {}

type EmbeddedChild struct{ EmbeddedBase }

func (EmbeddedChild) Ping() {}

func Speak(g Greeter) string {
	return g.Greet()
}

type CrossPackageGreeter struct{}

func (CrossPackageGreeter) ExternalGreet() string { return "external" }

var _ contract.ExternalGreeter = CrossPackageGreeter{}
