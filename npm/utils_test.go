package npm

import "testing"
import "reflect"

func TestGetLocalPath(t *testing.T) {
	tests := []struct {
		base     string
		path     string
		expected string
	}{
		{
			base:     "/var/lib/registry",
			path:     "react/-/react.tgz",
			expected: "/var/lib/registry/r/react/-/react.tgz",
		},
		{
			base:     "/var/lib/registry/",
			path:     "react/-/react.tgz",
			expected: "/var/lib/registry/r/react/-/react.tgz",
		},
		{
			base:     "/var/lib/registry/",
			path:     "/react/-/react.tgz",
			expected: "/var/lib/registry/r/react/-/react.tgz",
		},
	}

	for i, test := range tests {
		actual := getLocalPath(test.base, test.path)
		if actual != test.expected {
			t.Errorf("getLocalPath(%d): expected %s, actual %s", i, test.expected, actual)
		}
	}
}

func TestGetDistributions(t *testing.T) {
	tests := []struct {
		document string
		expected []*distribution
	}{
		{
			document: `{"_id": "test", "versions": {"0.0.1": {"dist": {"shasum": "3a16ee0d835eee3fbf97760efdfdbbe8fbfd4b3b", "tarball": "https://registry.npmjs.org/react/-/react.tgz"}}, "0.0.2": {"dist": {"shasum": "095de887016e2739a0773755f4ee6d8886c72ff3", "tarball": "https://registry.npmjs.org/react/-/react.tgz"}}}}`,
			expected: []*distribution{
				{
					SHA1:    "3a16ee0d835eee3fbf97760efdfdbbe8fbfd4b3b",
					Tarball: "https://registry.npmjs.org/react/-/react.tgz",
				},
				{
					SHA1:    "095de887016e2739a0773755f4ee6d8886c72ff3",
					Tarball: "https://registry.npmjs.org/react/-/react.tgz",
				},
			},
		},
	}

	for i, test := range tests {
		actual := getDistributions(test.document)
		if !reflect.DeepEqual(actual, test.expected) {
			t.Errorf("getDistributions(%d): expected %q actual %q", i, test.expected, actual)
		}
	}
}
