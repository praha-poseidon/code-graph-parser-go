package main

import "fmt"

type User struct {
	Name string
}

func (u User) Greet() string { return u.Name }

func main() {
	fmt.Println(User{Name: "a"}.Greet())
}
