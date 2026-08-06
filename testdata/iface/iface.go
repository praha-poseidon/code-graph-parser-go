package iface

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

func Speak(g Greeter) string {
	return g.Greet()
}
