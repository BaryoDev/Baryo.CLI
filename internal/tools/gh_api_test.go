// SPDX-License-Identifier: MIT

package tools

import "testing"

func TestValidateGhApi(t *testing.T) {
	ep := "repos/o/r/issues"
	cases := []struct {
		name string
		args []string
		ok   bool
	}{
		{"plain get", []string{ep}, true},
		{"explicit get", []string{ep, "-X", "GET"}, true},
		{"attached get", []string{ep, "-XGET"}, true},
		{"jq and paginate", []string{ep, "--paginate", "--jq", ".[].title"}, true},
		{"fields with explicit get", []string{ep, "-X", "GET", "-f", "state=open"}, true},
		{"short attached field with get", []string{ep, "-XGET", "-fstate=open"}, true},
		{"accept header", []string{ep, "-H", "Accept: application/vnd.github+json"}, true},

		{"post method", []string{ep, "-X", "POST"}, false},
		{"attached post", []string{ep, "-XPOST"}, false},
		{"method equals form", []string{ep, "--method=DELETE"}, false},
		{"-f alone", []string{ep, "-f", "title=x"}, false},
		{"--field equals", []string{ep, "--field=title=x"}, false},
		{"attached -f", []string{ep, "-ftitle=x"}, false},
		{"attached -F", []string{ep, "-Ftitle=x"}, false},
		{"--raw-field equals", []string{ep, "--raw-field=a=b"}, false},
		{"--input file", []string{ep, "--input", "body.json"}, false},
		{"--input equals", []string{ep, "--input=body.json"}, false},
		{"--input with get", []string{ep, "-X", "GET", "--input", "body.json"}, false},
		{"method override header", []string{ep, "-H", "X-HTTP-Method-Override: DELETE"}, false},
		{"file read field", []string{ep, "-X", "GET", "-F", "a=@/etc/passwd"}, false},
		{"graphql", []string{"graphql", "-X", "GET", "-f", "query={viewer{login}}"}, false},
		{"--field=k=@f", []string{ep, "-X", "GET", "--field=k=@f"}, false},
		{"--field k=@f", []string{ep, "-X", "GET", "--field", "k=@/etc/passwd"}, false},
		{"-Fk=@f", []string{ep, "-X", "GET", "-Fk=@f"}, false},
		{"-F k=@-", []string{ep, "-X", "GET", "-F", "k=@-"}, false},
		{"raw field @", []string{ep, "-X", "GET", "-f", "k=@f"}, false},
		{"hostname", []string{ep, "--hostname", "x"}, false},
		{"hostname equals", []string{ep, "--hostname=x"}, false},
		{"https endpoint", []string{"https://evil.example/repos/o/r"}, false},
		{"http endpoint", []string{"-X", "GET", "HTTP://evil.example/x"}, false},
		{"unknown flag", []string{ep, "--allow-escape-sequences"}, false},
		{"missing method value", []string{ep, "-X"}, false},
	}
	for _, c := range cases {
		err := validateGhApi(c.args)
		if c.ok && err != nil {
			t.Errorf("%s: want allowed, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: want refused, got nil", c.name)
		}
	}
}
