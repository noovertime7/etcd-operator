package file

import (
	"testing"
)

func TestParseBackupUrl(t *testing.T) {
	inputs := []string{
		"s3://mysqlbak/test.db",
		"s3://mysqlbak2/test2.db",
		"s3://mysqlbak3/test3.db",
	}
	for _, input := range inputs {
		_, err := ParseBackupUrl(input)
		if err != nil {
			t.Error(err)
		}
	}
}
