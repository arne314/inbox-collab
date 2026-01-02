package mail

import (
	"slices"
	"testing"
)

func Test_parseAddresses(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   []string
	}{
		{
			"none",
			"no email address here",
			[]string{},
		},
		{
			"simple",
			"mail@example.com",
			[]string{"mail@example.com"},
		},
		{
			"lower",
			"mAiL@examPle.COM",
			[]string{"mail@example.com"},
		},
		{
			"subdomain",
			"my-mail@my.subdomain.here.example.com",
			[]string{"my-mail@my.subdomain.here.example.com"},
		},
		{
			"first_name",
			"Name <mail@my.example.com>",
			[]string{"mail@my.example.com"},
		},
		{
			"surname",
			"My Name <my-mail@example.com>",
			[]string{"my-mail@example.com"},
		},
		{
			"missing_angles",
			"Some Name some-mail@example.com",
			[]string{"some-mail@example.com"},
		},
		{
			"special_characters",
			"!cool! name 1337 :) 1337-3.14@1337.example.com",
			[]string{"1337-3.14@1337.example.com"},
		},
		{
			"whitespaces",
			"  some \n whitespaces here \r <  \nmy-mail@example.com >",
			[]string{"my-mail@example.com"},
		},
		{
			"multiple",
			"<mail1@example.com>, mail2@example.com, Name 3 <mail3@example.com>",
			[]string{"mail1@example.com", "mail2@example.com", "mail3@example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAddresses(tt.header, true)
			if !slices.Equal(got, tt.want) {
				t.Errorf("parseAddresses() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseNameFrom(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			"empty",
			"<noname@example.com>",
			"",
		},
		{
			"empty_missing_angles",
			"noname@example.com",
			"",
		},
		{
			"simple",
			"Some Name <mail@example.com>",
			"Some Name",
		},
		{
			"quotes",
			"\"Some Name\" <mail@example.com>",
			"Some Name",
		},
		{
			"first_name",
			"First <mail@example.com>",
			"First",
		},
		{
			"missing_angles",
			"Some Name mail@example.com",
			"Some Name",
		},
		{
			"whitespaces",
			" additional  whitespaces  \n here \r <mail@example.com>",
			"additional whitespaces here",
		},
		{
			"special_characters",
			"!cool! name 1337 :) 1337-3.14@1337.example.com",
			"!cool! name 1337 :)",
		},
		{
			"multiple",
			"Person 1 <Person1@example.com>, Some Name mail@example.com, Other name <other@example.com>",
			"Person 1",
		},
		{
			"multiple",
			"Some Name <mail@example.com>, Other name <other@example.com>",
			"Some Name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNameFrom(tt.header)
			if got != tt.want {
				t.Errorf("parseNameFrom() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseIds(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   []string
	}{
		{
			"simple",
			"<id1-abc@example.com>",
			[]string{"id1-abc@example.com"},
		},
		{
			"subdomain",
			"<id1@some.subdomain.example.com>",
			[]string{"id1@some.subdomain.example.com"},
		},
		{
			"multiple",
			"<id1abc@example.com>, <id314@subdomain.example.com>",
			[]string{"id1abc@example.com", "id314@subdomain.example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIds(tt.header, true)
			if !slices.Equal(got, tt.want) {
				t.Errorf("parseIds() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseDomain(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			"simple",
			"<mail@example.com>",
			"example.com",
		},
		{
			"missing_angles",
			"mail@example.com",
			"example.com",
		},
		{
			"name",
			"My Name mail@example.com",
			"example.com",
		},
		{
			"quotes",
			"\"My Name\" mail@example.com",
			"example.com",
		},
		{
			"subdomain",
			"mail@some.subdomain.example.com",
			"some.subdomain.example.com",
		},
		{
			"whitespaces",
			" whitespaces\n here  mail@subdomain.example.com  ",
			"subdomain.example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDomain(tt.header)
			if got != tt.want {
				t.Errorf("parseDomain() = %v, want %v", got, tt.want)
			}
		})
	}
}
