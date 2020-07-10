package file

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/observiq/carbon/entry"
	"github.com/observiq/carbon/errors"
	"github.com/observiq/carbon/plugin/helper"
)

func ReadToEnd(ctx context.Context, path string, startOffset int64, lastSeenFileSize int64, messenger fileUpdateMessenger, splitFunc bufio.SplitFunc, pathField *entry.Field, inputPlugin helper.InputPlugin, maxLogSize int) error {
	defer messenger.FinishedReading()

	select {
	case <-ctx.Done():
		return nil
	default:
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}
	messenger.SetLastSeenFileSize(stat.Size())

	// Start at the beginning if the file has been truncated
	if stat.Size() < startOffset {
		startOffset = 0
		messenger.SetOffset(0)
	}

	_, err = file.Seek(startOffset, 0)
	if err != nil {
		return fmt.Errorf("seek file: %s", err)
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 16384)
	scanner.Buffer(buf, maxLogSize)
	pos := startOffset
	scanFunc := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		advance, token, err = splitFunc(data, atEOF)
		pos += int64(advance)
		return
	}
	scanner.Split(scanFunc)

	// If we're not at the end of the file, and we haven't
	// advanced since last cycle, read the rest of the file as an entry
	defer func() {
		if pos < stat.Size() && pos == startOffset && lastSeenFileSize == stat.Size() {
			_, err := file.Seek(pos, 0)
			if err != nil {
				inputPlugin.Errorf("failed to seek to read last log entry")
				return
			}
			msgBuf := make([]byte, stat.Size()-pos)
			n, err := file.Read(msgBuf)
			if err != nil {
				inputPlugin.Errorf("failed to read trailing log")
				return
			}
			e := inputPlugin.NewEntry(string(msgBuf[:n]))
			inputPlugin.Write(ctx, e)
			messenger.SetOffset(pos + int64(n))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		ok := scanner.Scan()
		if !ok {
			if err := scanner.Err(); err == bufio.ErrTooLong {
				return errors.NewError("log entry too large", "increase max_log_size or ensure that multiline regex patterns terminate")
			} else {
				return scanner.Err()
			}
		}

		message := scanner.Text()

		entry := entry.New()
		entry.Set(inputPlugin.WriteTo, message)
		if pathField != nil {
			entry.Set(*pathField, path)
		}

		inputPlugin.Write(ctx, entry)
		messenger.SetOffset(pos)
	}
}
