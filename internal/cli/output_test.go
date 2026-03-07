package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrintRows(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, printRows(&buf, []string{"name", "count"}, [][]string{{"general", "5"}}))
	require.Contains(t, buf.String(), "name")
	require.Contains(t, buf.String(), "general")
}
