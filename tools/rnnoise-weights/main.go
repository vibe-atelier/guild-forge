// Command rnnoise-weights turns RNNoise's trained model (rnnoise_data.c,
// from Xiph's rnnoise_data-<version>.tar.gz) into the compact weights file
// RNNoise reads at run time (rnnoise_model_from_buffer), in the format of
// RNNoise's own write_weights.c. Nexus builds RNNoise with USE_WEIGHTS_FILE,
// so the program itself doesn't carry the 29 MB of numbers.
//
// It is pure Go: it reads the numbers from the C text, so nothing has to be
// compiled or linked.
//
//	go run ./tools/rnnoise-weights <rnnoise_data.c> <weights file to write> [<slim C file to write>]
//
// The optional slim C file is rnnoise_data.c without the number lists
// (what USE_WEIGHTS_FILE leaves anyway): the code that finds them in the
// weights file.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	blockSize   = 64 // It is WEIGHT_BLOCK_SIZE.
	blobVersion = 0  // It is WEIGHT_BLOB_VERSION.
)

var (
	arrayStart = regexp.MustCompile(`^static const (float|opus_int8|int) (\w+)\[(\d+)\] = \{`)
	typeDef    = regexp.MustCompile(`^#define WEIGHTS_(\w+)_TYPE WEIGHT_TYPE_(\w+)`)
	listEntry  = regexp.MustCompile(`^\s*\{"(\w+)",`)
	weightType = map[string]int32{"float": 0, "int": 1, "qweight": 2, "int8": 3}
)

type array struct {
	ctype string
	data  bytes.Buffer
	count int
	want  int
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: rnnoise-weights <rnnoise_data.c> <weights file> [<slim C file>]")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3:]); err != nil {
		fmt.Fprintln(os.Stderr, "rnnoise-weights:", err)
		os.Exit(1)
	}
}

func run(src, out string, slim []string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	arrays := map[string]*array{}
	types := map[string]string{}
	var order []string
	var cur *array
	var slimText strings.Builder
	depth := 0 // It counts depth inside "#ifndef USE_WEIGHTS_FILE" blocks (left out of the slim file).
	inList := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		switch {
		case trim == "#ifndef USE_WEIGHTS_FILE":
			depth++
		case strings.HasPrefix(trim, "#endif /* USE_WEIGHTS_FILE */"):
			depth--
			continue
		}
		if depth == 0 {
			slimText.WriteString(line + "\n")
		}
		if m := typeDef.FindStringSubmatch(trim); m != nil {
			types[m[1]] = m[2]
		}
		if cur != nil {
			if strings.HasPrefix(trim, "};") {
				cur = nil
				continue
			}
			for _, tok := range strings.Split(trim, ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				if err := cur.add(tok); err != nil {
					return err
				}
			}
			continue
		}
		if m := arrayStart.FindStringSubmatch(trim); m != nil {
			n, _ := strconv.Atoi(m[3])
			cur = &array{ctype: m[1], want: n}
			arrays[m[2]] = cur
			continue
		}
		if strings.HasPrefix(trim, "const WeightArray rnnoise_arrays[]") {
			inList = true
		}
		if inList {
			if m := listEntry.FindStringSubmatch(line); m != nil {
				order = append(order, m[1])
			}
			if strings.HasPrefix(trim, "};") {
				inList = false
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(order) == 0 {
		return fmt.Errorf("no rnnoise_arrays list in %s", src)
	}
	var blob bytes.Buffer
	for _, name := range order {
		a := arrays[name]
		if a == nil {
			continue // It is left out of this model (like the C list's #ifdef).
		}
		if a.count != a.want {
			return fmt.Errorf("%s: %d numbers, expected %d", name, a.count, a.want)
		}
		t, ok := weightType[types[name]]
		if !ok {
			return fmt.Errorf("%s: unknown kind %q", name, types[name])
		}
		size := a.data.Len()
		head := make([]byte, blockSize)
		copy(head, "DNNw")
		binary.LittleEndian.PutUint32(head[4:], blobVersion)
		binary.LittleEndian.PutUint32(head[8:], uint32(t))
		binary.LittleEndian.PutUint32(head[12:], uint32(size))
		block := (size + blockSize - 1) / blockSize * blockSize
		binary.LittleEndian.PutUint32(head[16:], uint32(block))
		if len(name) >= 43 {
			return fmt.Errorf("%s: name too long", name)
		}
		copy(head[20:], name)
		blob.Write(head)
		blob.Write(a.data.Bytes())
		blob.Write(make([]byte, block-size))
	}
	if err := os.WriteFile(out, blob.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d lists, %d bytes)\n", out, len(order), blob.Len())
	if len(slim) > 0 {
		text := "/* rnnoise_data.c without its number lists (USE_WEIGHTS_FILE): made by\n   tools/rnnoise-weights for Nexus. The numbers are read from the weights file. */\n" + slimText.String()
		if err := os.WriteFile(slim[0], []byte(text), 0o644); err != nil {
			return err
		}
		fmt.Printf("Wrote %s\n", slim[0])
	}
	return nil
}

// add appends one number in the array's own C type (little-endian, as on
// x86 and ARM computers).
func (a *array) add(tok string) error {
	tok = strings.TrimSuffix(strings.TrimSuffix(tok, "f"), "F")
	a.count++
	switch a.ctype {
	case "float":
		v, err := strconv.ParseFloat(tok, 32)
		if err != nil {
			return fmt.Errorf("bad number %q", tok)
		}
		binary.Write(&a.data, binary.LittleEndian, math.Float32bits(float32(v)))
	case "opus_int8":
		v, err := strconv.ParseInt(tok, 0, 8)
		if err != nil {
			return fmt.Errorf("bad number %q", tok)
		}
		a.data.WriteByte(byte(int8(v)))
	case "int":
		v, err := strconv.ParseInt(tok, 0, 32)
		if err != nil {
			return fmt.Errorf("bad number %q", tok)
		}
		binary.Write(&a.data, binary.LittleEndian, int32(v))
	}
	return nil
}
