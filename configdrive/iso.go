package configdrive

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"subuk/vmango/util"
)

type CmdIsoFileReader struct {
	filename string
	files    map[string]struct{}
	joilet   bool
	rock     bool
}

func (reader *CmdIsoFileReader) ReadFile(filename string) ([]byte, error) {
	if _, exists := reader.files[filename]; !exists {
		return nil, fmt.Errorf("file %s not found", filename)
	}
	args := []string{"-x", filename, "-i", reader.filename}
	if reader.joilet {
		args = append(args, "-J")
	}
	if reader.rock {
		args = append(args, "-R")
	}
	cmd := exec.Command("isoinfo", args...)
	bytes, err := cmd.Output()
	if err != nil {
		return nil, util.NewError(err, "extract iso file cmd failed: '%s'", strings.Join(cmd.Args, " "))
	}
	return bytes, nil
}

func NewCmdIsoFileReader(filename string) (*CmdIsoFileReader, error) {
	capsCmd := exec.Command("isoinfo", "-d", "-i", filename)
	capsCmdBytes, err := capsCmd.Output()
	if err != nil {
		return nil, util.NewError(err, "cannot fetch capabilities with '%s'", strings.Join(capsCmd.Args, " "))
	}
	reader := &CmdIsoFileReader{filename: filename, files: map[string]struct{}{}}
	capsScanner := bufio.NewScanner(bytes.NewBuffer(capsCmdBytes))
	for capsScanner.Scan() {
		line := strings.TrimSpace(capsScanner.Text())
		if strings.HasPrefix(line, "Joilet ") {
			reader.joilet = true
		}
		if strings.HasPrefix(line, "Rock Ridge ") {
			reader.rock = true
		}
	}
	if err := capsScanner.Err(); err != nil {
		return nil, util.NewError(err, "cannot scan capabilities")
	}

	filesCmd := exec.Command("isoinfo", "-f", "-i", filename)
	if reader.joilet {
		filesCmd.Args = append(filesCmd.Args, "-J")
	}
	if reader.rock {
		filesCmd.Args = append(filesCmd.Args, "-R")
	}
	filesCmdBytes, err := filesCmd.Output()
	if err != nil {
		return nil, util.NewError(err, "cannot list files with '%s'", strings.Join(filesCmd.Args, " "))
	}
	filesScanner := bufio.NewScanner(bytes.NewReader(filesCmdBytes))
	for filesScanner.Scan() {
		filename := strings.TrimSpace(filesScanner.Text())
		reader.files[filename] = struct{}{}
	}
	if err := filesScanner.Err(); err != nil {
		return nil, util.NewError(err, "cannot scan file list")
	}
	return reader, nil
}

func ParseIso(formats []Format, reader io.Reader) (Data, error) {
	tmpfile, err := ioutil.TempFile("", "configdrive-parse")
	if err != nil {
		return nil, util.NewError(err, "cannot create tmp file")
	}
	defer tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	if _, err := io.Copy(tmpfile, reader); err != nil {
		return nil, util.NewError(err, "download failed")
	}

	isoReader, err := NewCmdIsoFileReader(tmpfile.Name())
	if err != nil {
		return nil, util.NewError(err, "cannot initialize iso file reader")
	}

	errs := []string{}
	for _, format := range formats {
		switch format {
		default:
			errs = append(errs, fmt.Sprintf("%s: unknown data format", format))
			continue
		case FormatOpenstack:
			data := &Openstack{}
			mdBytes, err := isoReader.ReadFile("/openstack/latest/meta_data.json")
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: cannot extract metadata from file: %s", format, err))
				continue
			}
			if err := (&data.Metadata).Unmarshal(mdBytes); err != nil {
				errs = append(errs, fmt.Sprintf("%s: cannot parse metadata: %s", format, err))
				continue
			}
			udBytes, err := isoReader.ReadFile("/openstack/latest/user_data")
			if err == nil {
				data.Userdata = udBytes
			}
			return data, nil
		case FormatNoCloud:
			data := &NoCloud{}
			mdBytes, err := isoReader.ReadFile("/meta-data")
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: cannot extract metadata from file: %s", format, err))
				continue
			}
			if err := (&data.Metadata).Unmarshal(mdBytes); err != nil {
				errs = append(errs, fmt.Sprintf("%s: cannot parse metadata: %s", format, err))
				continue
			}
			udBytes, err := isoReader.ReadFile("/user-data")
			if err == nil {
				data.Userdata = udBytes
			}
			return data, nil
		}
	}
	return nil, errors.New("[" + strings.Join(errs, ", ") + "]")
}

func GenerateIso(idata Data) (*os.File, error) {
	tmpdir, err := ioutil.TempDir("", "vmango-configdrive-iso-content")
	if err != nil {
		return nil, util.NewError(err, "cannot create tmp directory")
	}
	defer os.RemoveAll(tmpdir)

	localConfigdriveFilename := filepath.Join(tmpdir, "drive.iso")
	switch data := idata.(type) {
	default:
		panic(fmt.Errorf("unknown cloud-init data type %T", data))
	case *Openstack:
		mdBytes, err := data.Metadata.Marshal()
		if err != nil {
			return nil, util.NewError(err, "cannot marshal config metadata")
		}
		if err := os.MkdirAll(filepath.Join(tmpdir, "openstack/latest"), 0755); err != nil {
			return nil, util.NewError(err, "cannot create openstack folder for configdrive")
		}
		if err := ioutil.WriteFile(filepath.Join(tmpdir, "openstack/latest/meta_data.json"), mdBytes, 0644); err != nil {
			return nil, util.NewError(err, "cannot write metadata file to config drive")
		}
		if err := ioutil.WriteFile(filepath.Join(tmpdir, "openstack/latest/user_data"), data.Userdata, 0644); err != nil {
			return nil, util.NewError(err, "cannot write userdata file to config drive")
		}
		cmd := exec.Command("mkisofs", "-o", localConfigdriveFilename, "-V", "config-2", "-R", "--quiet", tmpdir)
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		file, err := os.Open(localConfigdriveFilename)
		if err != nil {
			return nil, util.NewError(err, "cannot open local generated iso")
		}
		return file, nil
	case *NoCloud:
		mdBytes, err := data.Metadata.Marshal()
		if err != nil {
			return nil, util.NewError(err, "cannot marshal config metadata")
		}
		if err := ioutil.WriteFile(filepath.Join(tmpdir, "meta-data"), mdBytes, 0644); err != nil {
			return nil, util.NewError(err, "cannot write metadata file to config drive")
		}
		if err := ioutil.WriteFile(filepath.Join(tmpdir, "user-data"), data.Userdata, 0644); err != nil {
			return nil, util.NewError(err, "cannot write userdata file to config drive")
		}
		cmd := exec.Command("mkisofs", "-o", localConfigdriveFilename, "-V", "CIDATA", "-r", "-J", "--quiet", tmpdir)
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		file, err := os.Open(localConfigdriveFilename)
		if err != nil {
			return nil, util.NewError(err, "cannot open local generated iso")
		}
		return file, nil
	}

}
