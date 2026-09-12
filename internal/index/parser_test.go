// SPDX-License-Identifier: MIT

package index

import (
	"testing"
)

func TestExtractRust(t *testing.T) {
	src := []byte(`
fn main() {
    println!("hello");
}

struct Point {
    x: f64,
    y: f64,
}

enum Color {
    Red,
    Green,
    Blue,
}

trait Drawable {
    fn draw(&self);
}

impl Point {
    fn new(x: f64, y: f64) -> Point {
        Point { x, y }
    }

    fn distance(&self, other: &Point) -> f64 {
        0.0
    }
}
`)
	fs, err := ParseFile("test.rs", "rust", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"main":     KindFunction,
		"Point":    KindType,
		"Color":    KindType,
		"Drawable": KindInterface,
		"new":      KindMethod,
		"distance": KindMethod,
	}

	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok {
			if s.Kind != k {
				t.Errorf("%s: got kind %d, want %d", s.Name, s.Kind, k)
			}
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}

	// Check impl methods have correct parent.
	for _, s := range fs.Symbols {
		if s.Name == "new" || s.Name == "distance" {
			if s.Parent != "Point" {
				t.Errorf("%s: got parent %q, want %q", s.Name, s.Parent, "Point")
			}
		}
	}
}

func TestExtractJava(t *testing.T) {
	src := []byte(`
public class Calculator {
    public int add(int a, int b) {
        return a + b;
    }

    public Calculator() {
    }
}

interface Computable {
    int compute(int x);
}
`)
	fs, err := ParseFile("Test.java", "java", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"Calculator": KindClass,
		"add":        KindMethod,
		"Computable": KindInterface,
	}

	// Note: the constructor is also emitted as a method named "Calculator",
	// so check that each expected (name, kind) pair exists rather than
	// requiring every symbol with a given name to have one kind.
	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok && s.Kind == k {
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}

	// Check method parent.
	for _, s := range fs.Symbols {
		if s.Name == "add" {
			if s.Parent != "Calculator" {
				t.Errorf("add: got parent %q, want %q", s.Parent, "Calculator")
			}
		}
	}
}

func TestExtractC(t *testing.T) {
	src := []byte(`
struct Point {
    double x;
    double y;
};

enum Color {
    RED,
    GREEN,
    BLUE
};

int add(int a, int b) {
    return a + b;
}

void print_point(struct Point* p) {
    printf("%f %f\n", p->x, p->y);
}
`)
	fs, err := ParseFile("test.c", "c", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"Point":       KindType,
		"Color":       KindType,
		"add":         KindFunction,
		"print_point": KindFunction,
	}

	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok {
			if s.Kind != k {
				t.Errorf("%s: got kind %d, want %d", s.Name, s.Kind, k)
			}
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}
}

func TestExtractCPP(t *testing.T) {
	src := []byte(`
class Shape {
public:
    void draw() {
        // ...
    }
    int area();
};

struct Vec2 {
    float x, y;
};

enum Direction {
    UP,
    DOWN,
    LEFT,
    RIGHT
};

int main() {
    return 0;
}
`)
	fs, err := ParseFile("test.cpp", "cpp", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"Shape":     KindClass,
		"Vec2":      KindType,
		"Direction": KindType,
		"main":      KindFunction,
		"draw":      KindMethod,
	}

	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok {
			if s.Kind != k {
				t.Errorf("%s: got kind %d, want %d", s.Name, s.Kind, k)
			}
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}

	// Check class method parent.
	for _, s := range fs.Symbols {
		if s.Name == "draw" {
			if s.Parent != "Shape" {
				t.Errorf("draw: got parent %q, want %q", s.Parent, "Shape")
			}
		}
	}
}

func TestLangForFileNewExtensions(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"main.rs", "rust"},
		{"App.java", "java"},
		{"main.c", "c"},
		{"util.h", "c"},
		{"main.cpp", "cpp"},
		{"main.cc", "cpp"},
		{"main.cxx", "cpp"},
		{"util.hpp", "cpp"},
	}
	for _, tc := range cases {
		got := LangForFile(tc.path)
		if got != tc.want {
			t.Errorf("LangForFile(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestExtractGo(t *testing.T) {
	src := []byte(`
package main

type Greeter interface {
	Greet() string
}

type User struct {
	Name string
}

func (u *User) Greet() string {
	return "Hello " + u.Name
}

func NewUser(name string) *User {
	return &User{Name: name}
}
`)
	fs, err := ParseFile("main.go", "go", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"Greeter": KindInterface,
		"User":    KindType,
		"Greet":   KindMethod,
		"NewUser": KindFunction,
	}

	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok && s.Kind == k {
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}
}

func TestExtractJS(t *testing.T) {
	src := []byte(`
function add(a, b) {
	return a + b;
}

class Person {
	greet() {
		return "hello";
	}
}

const multiply = (x, y) => x * y;
`)
	fs, err := ParseFile("test.js", "javascript", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"add":      KindFunction,
		"Person":   KindClass,
		"greet":    KindMethod,
		"multiply": KindFunction,
	}

	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok && s.Kind == k {
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}
}

func TestExtractTS(t *testing.T) {
	src := []byte(`
interface Animal {
	speak(): void;
}

type ID = string | number;

class Dog {
	speak() {}
}

function bark() {}
`)
	fs, err := ParseFile("test.ts", "typescript", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"Animal": KindInterface,
		"ID":     KindType,
		"Dog":    KindClass,
		"speak":  KindMethod,
		"bark":   KindFunction,
	}

	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok && s.Kind == k {
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}
}

func TestExtractPython(t *testing.T) {
	src := []byte(`
def add(a, b):
    return a + b

class Animal:
    def speak(self):
        pass
`)
	fs, err := ParseFile("test.py", "python", src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]SymbolKind{
		"add":    KindFunction,
		"Animal": KindClass,
		"speak":  KindMethod,
	}

	found := make(map[string]bool)
	for _, s := range fs.Symbols {
		if k, ok := want[s.Name]; ok && s.Kind == k {
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol: %s", name)
		}
	}
}
