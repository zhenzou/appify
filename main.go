package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/JackMordaunt/icns"
	"github.com/pkg/errors"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(2)
	}
}

func run() error {
	var (
		name       = flag.String("name", "My Go Application", "app name")
		author     = flag.String("author", "Appify by Machine Box", "author")
		version    = flag.String("version", "1.0", "app version")
		identifier = flag.String("id", "", "bundle identifier")
		icon       = flag.String("icon", "", "icon image file (.icns|.png|.jpg|.jpeg)")
		mode       = flag.String("mode", "normal", "mode (normal,tray)")
	)
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		return errors.New("missing executable argument")
	}
	bin := args[0]
	appName := *name + ".app"

	contentsPath := filepath.Join(appName, "Contents")
	appPath := filepath.Join(contentsPath, "MacOS")
	resourcesPath := filepath.Join(contentsPath, "Resources")
	binPath := filepath.Join(appPath, appName)

	if err := os.MkdirAll(appPath, 0777); err != nil {
		return errors.Wrap(err, "os.MkdirAll appPath")
	}
	fdst, err := os.Create(binPath)
	if err != nil {
		return errors.Wrap(err, "create bin")
	}
	defer fdst.Close()
	fsrc, err := os.Open(bin)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New(bin + " not found")
		}
		return errors.Wrap(err, "os.Open")
	}
	defer fsrc.Close()
	if _, err := io.Copy(fdst, fsrc); err != nil {
		return errors.Wrap(err, "copy bin")
	}
	if err := exec.Command("chmod", "+x", appPath).Run(); err != nil {
		return errors.Wrap(err, "chmod: "+appPath)
	}
	if err := exec.Command("chmod", "+x", binPath).Run(); err != nil {
		return errors.Wrap(err, "chmod: "+binPath)
	}
	id := *identifier
	if id == "" {
		id = *author + "." + *name
	}
	info := infoListData{
		Name:               *name,
		Executable:         filepath.Join("MacOS", appName),
		Identifier:         id,
		Version:            *version,
		InfoString:         *name + " by " + *author,
		ShortVersionString: *version,
		Mode:               *mode,
	}
	if *icon != "" {
		iconPath, err := prepareIcons(*icon, resourcesPath)
		if err != nil {
			return errors.Wrap(err, "icon")
		}
		info.IconFile = filepath.Base(iconPath)
	}
	tpl, err := template.New("template").Parse(infoPlistTemplate)
	if err != nil {
		return errors.Wrap(err, "infoPlistTemplate")
	}
	fplist, err := os.Create(filepath.Join(contentsPath, "Info.plist"))
	if err != nil {
		return errors.Wrap(err, "create Info.plist")
	}
	defer fplist.Close()
	if err := tpl.Execute(fplist, info); err != nil {
		return errors.Wrap(err, "execute Info.plist template")
	}
	if err := ioutil.WriteFile(filepath.Join(contentsPath, "README"), []byte(readme), 0666); err != nil {
		return errors.Wrap(err, "ioutil.WriteFile")
	}

	return buildDMG(*name, appName)
}

func prepareIcons(iconPath, resourcesPath string) (string, error) {
	ext := filepath.Ext(strings.ToLower(iconPath))
	fsrc, err := os.Open(iconPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("icon file not found")
		}
		return "", errors.Wrap(err, "open icon file")
	}
	defer fsrc.Close()
	if err := os.MkdirAll(resourcesPath, 0777); err != nil {
		return "", errors.Wrap(err, "os.MkdirAll resourcesPath")
	}
	destFile := filepath.Join(resourcesPath, "icon.icns")
	fdst, err := os.Create(destFile)
	if err != nil {
		return "", errors.Wrap(err, "create icon.icns file")
	}
	defer fdst.Close()
	switch ext {
	case ".icns": // just copy the .icns file
		_, err := io.Copy(fdst, fsrc)
		if err != nil {
			return destFile, errors.Wrap(err, "copying "+iconPath)
		}
	case ".png", ".jpg", ".jpeg", ".gif": // process any images
		srcImg, _, err := image.Decode(fsrc)
		if err != nil {
			return destFile, errors.Wrap(err, "decode image")
		}
		if err := icns.Encode(fdst, srcImg); err != nil {
			return destFile, errors.Wrap(err, "generate icns file")
		}
	default:
		return destFile, errors.New(ext + " icons not supported")
	}
	return destFile, nil
}

func buildDMG(appName, appDir string) error {
	dmg := makeTemplateDMG()
	return buildDMGFromTemplate(appName, dmg, appDir)
}

func makeTemplateDMG() string {
	templateDMG := "template"

	err := os.Mkdir("tmp", os.ModePerm)
	if err != nil {
		println("mkdir tmp dir error")
		os.Exit(-1)
	}
	defer os.Remove("./tmp")

	// create the template dmg
	cmd := exec.Command("hdiutil",
		"create",
		"-fs", "HFSX",
		"-layout", "SPUD",
		"-size", "10m",
		templateDMG,
		"-srcfolder", "tmp",
		"-format", "UDRW",
		//"-volname tmp",
		"-quiet",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Println("create dmg file error:", err.Error())
		os.Exit(-1)
	}
	return templateDMG + ".dmg"
}

// copy from https://gist.github.com/mholt/11008646c95d787c30806d3f24b2c844
func buildDMGFromTemplate(appName, templateDMG, appDir string) error {

	tmpDir := "./tmp"
	err := os.Mkdir(tmpDir, 0755)
	if err != nil {
		return fmt.Errorf("making temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// attach the template dmg
	cmd := exec.Command("hdiutil", "attach", templateDMG, "-noautoopen", "-mountpoint", tmpDir)
	attachBuf := new(bytes.Buffer)
	cmd.Stdout = attachBuf
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("running hdiutil attach: %v", err)
	}

	err = deepCopy(appDir, tmpDir)
	if err != nil {
		return fmt.Errorf("copying app into dmg: %v", err)
	}

	// get attached image's device; it should be the
	// first device that is outputted
	hdiutilOutFields := strings.Fields(attachBuf.String())
	if len(hdiutilOutFields) == 0 {
		return fmt.Errorf("no device output by hdiutil attach")
	}
	dmgDevice := hdiutilOutFields[0]

	// detach image
	cmd = exec.Command("hdiutil", "detach", dmgDevice)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("running hdiutil detach: %v", err)
	}

	// convert to compressed image
	outputDMG := appName + ".dmg"
	cmd = exec.Command("hdiutil", "convert", templateDMG, "-format", "UDZO", "-imagekey", "zlib-level=9", "-o", outputDMG)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("running hdiutil convert: %v", err)
	}

	return nil
}

// deepCopy makes a deep copy of from into to.
func deepCopy(from, to string) error {
	if from == "" || to == "" {
		return fmt.Errorf("no source or no destination; both required")
	}

	// traverse the source directory and copy each file
	return filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		// error accessing current file
		if err != nil {
			return err
		}

		// skip files/folders without a name
		if info.Name() == "" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// if directory, create destination directory (if not
		// already created by our pre-walk)
		if info.IsDir() {
			subdir := strings.TrimPrefix(path, filepath.Dir(from))
			destDir := filepath.Join(to, subdir)
			if _, err := os.Stat(destDir); os.IsNotExist(err) {
				err := os.Mkdir(destDir, info.Mode()&os.ModePerm)
				if err != nil {
					return err
				}
			}
			return nil
		}

		destPath := filepath.Join(to, strings.TrimPrefix(path, filepath.Dir(from)))
		err = copyFile(path, destPath, info)
		if err != nil {
			return fmt.Errorf("copying file %s: %v", path, err)
		}
		return nil
	})
}

func copyFile(from, to string, fromInfo os.FileInfo) error {
	log.Printf("[INFO] Copying %s to %s", from, to)

	if fromInfo == nil {
		var err error
		fromInfo, err = os.Stat(from)
		if err != nil {
			return err
		}
	}

	// open source file
	fsrc, err := os.Open(from)
	if err != nil {
		return err
	}

	// create destination file, with identical permissions
	fdest, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fromInfo.Mode()&os.ModePerm)
	if err != nil {
		fsrc.Close()
		if _, err2 := os.Stat(to); err2 == nil {
			return fmt.Errorf("opening destination (which already exists): %v", err)
		}
		return err
	}

	// copy the file and ensure it gets flushed to disk
	if _, err = io.Copy(fdest, fsrc); err != nil {
		fsrc.Close()
		fdest.Close()
		return err
	}
	if err = fdest.Sync(); err != nil {
		fsrc.Close()
		fdest.Close()
		return err
	}

	// close both files
	if err = fsrc.Close(); err != nil {
		fdest.Close()
		return err
	}
	if err = fdest.Close(); err != nil {
		return err
	}

	return nil
}

type infoListData struct {
	Name               string
	Executable         string
	Identifier         string
	Version            string
	InfoString         string
	ShortVersionString string
	IconFile           string
	Mode               string
}

const infoPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
	<dict>
		<key>CFBundlePackageType</key>
		<string>APPL</string>
		<key>CFBundleInfoDictionaryVersion</key>
		<string>6.0</string>
		<key>CFBundleName</key>
		<string>{{ .Name }}</string>
		<key>CFBundleExecutable</key>
		<string>{{ .Executable }}</string>
		<key>CFBundleIdentifier</key>
		<string>{{ .Identifier }}</string>
		<key>CFBundleVersion</key>
		<string>{{ .Version }}</string>
		<key>CFBundleGetInfoString</key>
		<string>{{ .InfoString }}</string>
		<key>CFBundleShortVersionString</key>
		<string>{{ .ShortVersionString }}</string>
		{{ if .IconFile -}}
		<key>CFBundleIconFile</key>
		<string>{{ .IconFile }}</string>
		{{- end }}
		<key>NSHighResolutionCapable</key>
		<true/>
		{{ if eq .Mode "tray" -}}
		<key>LSUIElement</key>
		<true/>
		{{- end }}
	</dict>
</plist>
`

// readme goes into a README file inside the package for
// future reference.
const readme = `Made with Appify by Machine Box
https://github.com/machinebox/appify

Inspired by https://gist.github.com/anmoljagetia/d37da67b9d408b35ac753ce51e420132 
`
