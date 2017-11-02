package move

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path"
	"strings"
	"time"

	"github.com/cozy/cozy-stack/pkg/consts"
	"github.com/cozy/cozy-stack/pkg/couchdb"
	"github.com/cozy/cozy-stack/pkg/instance"
	"github.com/cozy/cozy-stack/pkg/logger"
	"github.com/cozy/cozy-stack/pkg/vfs"
	vcardParser "github.com/emersion/go-vcard"
)

// ContactName is a struct describing a name of a contact
type ContactName struct {
	FamilyName     string `json:"familyName,omitempty"`
	GivenName      string `json:"givenName,omitempty"`
	AdditionalName string `json:"additionalName,omitempty"`
	NamePrefix     string `json:"namePrefix,omitempty"`
	NameSuffix     string `json:"nameSuffix,omitempty"`
}

// ContactEmail is a struct describing an email of a contact
type ContactEmail struct {
	Address string `json:"address"`
	Type    string `json:"type,omitempty"`
	Label   string `json:"label,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// ContactAddress is a struct describing an address of a contact
type ContactAddress struct {
	Street           string `json:"street,omitempty"`
	Pobox            string `json:"pobox,omitempty"`
	City             string `json:"city,omitempty"`
	Region           string `json:"region,omitempty"`
	Postcode         string `json:"postcode,omitempty"`
	Country          string `json:"country,omitempty"`
	Type             string `json:"type,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
	Label            string `json:"label,omitempty"`
	FormattedAddress string `json:"formattedAddress,omitempty"`
}

// ContactPhone is a struct describing a phone of a contact
type ContactPhone struct {
	Number  string `json:"number"`
	Type    string `json:"type,omitempty"`
	Label   string `json:"label,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// ContactCozy is a struct describing a cozy instance of a contact
type ContactCozy struct {
	URL     string `json:"url"`
	Label   string `json:"label,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// Contact is a struct containing all the informations about a contact
type Contact struct {
	DocID  string `json:"_id,omitempty"`
	DocRev string `json:"_rev,omitempty"`

	FullName string           `json:"fullname,omitempty"`
	Name     ContactName      `json:"name,omitempty"`
	Birthday string           `json:"birthday,omitempty"`
	Note     string           `json:"note,omitempty"`
	Email    []ContactEmail   `json:"email,omitempty"`
	Address  []ContactAddress `json:"address,omitempty"`
	Phone    []ContactPhone   `json:"phone,omitempty"`
	Cozy     []ContactCozy    `json:"cozy,omitempty"`
}

// ID returns the contact qualified identifier
func (c *Contact) ID() string { return c.DocID }

// Rev returns the contact revision
func (c *Contact) Rev() string { return c.DocRev }

// DocType returns the contact document type
func (c *Contact) DocType() string { return consts.Contacts }

// Clone implements couchdb.Doc
func (c *Contact) Clone() couchdb.Doc {
	cloned := *c
	cloned.FullName = c.FullName
	cloned.Name = c.Name

	cloned.Email = make([]ContactEmail, len(c.Email))
	copy(cloned.Email, c.Email)

	cloned.Address = make([]ContactAddress, len(c.Address))
	copy(cloned.Address, c.Address)

	cloned.Phone = make([]ContactPhone, len(c.Phone))
	copy(cloned.Phone, c.Phone)

	cloned.Cozy = make([]ContactCozy, len(c.Cozy))
	copy(cloned.Cozy, c.Cozy)

	return &cloned
}

// SetID changes the contact qualified identifier
func (c *Contact) SetID(id string) { c.DocID = id }

// SetRev changes the contact revision
func (c *Contact) SetRev(rev string) { c.DocRev = rev }

func createContact(fs vfs.VFS, hdr *tar.Header, tr *tar.Reader, db couchdb.Database) error {
	decoder := vcardParser.NewDecoder(tr)
	vcard, err := decoder.Decode()
	if err != nil {
		return err
	}

	fullname := "John Doe"
	contactname := ContactName{
		GivenName:  "John",
		FamilyName: "Doe",
	}

	name := vcard.Name()
	if name != nil {
		contactname = ContactName{
			FamilyName:     name.FamilyName,
			GivenName:      name.GivenName,
			AdditionalName: name.AdditionalName,
			NamePrefix:     name.HonorificPrefix,
			NameSuffix:     name.HonorificSuffix,
		}
		fullname = name.Value
	}
	if names := vcard.FormattedNames(); len(names) > 0 {
		fullname = names[0].Value
	}

	var bday string
	if field := vcard.Get("BDAY"); field != nil {
		bday = field.Value
	}

	var note string
	if field := vcard.Get("NOTE"); field != nil {
		note = field.Value
	}

	var contactemail []ContactEmail
	for _, mail := range vcard.Values("EMAIL") {
		ce := ContactEmail{
			Address: mail,
		}
		contactemail = append(contactemail, ce)
	}

	var contactphone []ContactPhone
	for _, phone := range vcard.Values("TEL") {
		cp := ContactPhone{
			Number: phone,
		}
		contactphone = append(contactphone, cp)
	}

	var contactaddress []ContactAddress
	for _, address := range vcard.Addresses() {
		ca := ContactAddress{
			Street:           address.StreetAddress,
			Pobox:            address.PostOfficeBox,
			City:             address.Locality,
			Region:           address.Region,
			Postcode:         address.PostalCode,
			Country:          address.Country,
			FormattedAddress: address.Value,
		}
		contactaddress = append(contactaddress, ca)
	}

	contact := &Contact{
		FullName: fullname,
		Name:     contactname,
		Birthday: bday,
		Note:     note,
		Address:  contactaddress,
		Email:    contactemail,
		Phone:    contactphone,
	}

	return couchdb.CreateDoc(db, contact)
}

func createAlbums(i *instance.Instance, tr *tar.Reader, albums *AlbumReferences) error {
	bs := bufio.NewScanner(tr)

	for bs.Scan() {
		jsondoc := &couchdb.JSONDoc{}
		if err := jsondoc.UnmarshalJSON(bs.Bytes()); err != nil {
			return err
		}
		delete(jsondoc.M, "type")
		id := jsondoc.ID()
		jsondoc.SetID("")
		jsondoc.SetRev("")
		jsondoc.Type = consts.PhotosAlbums

		if err := couchdb.CreateDoc(i, jsondoc); err != nil {
			return err
		}
		(*albums)[id] = couchdb.DocReference{
			ID:   jsondoc.ID(),
			Type: consts.PhotosAlbums,
		}
	}

	return nil
}

// AlbumReferences is used to associate photos to their albums, though we don't
// force the ID of the albums to the values in the tarball.
type AlbumReferences map[string]couchdb.DocReference

func fillAlbums(i *instance.Instance, tr *tar.Reader, dstDoc *vfs.DirDoc, albums *AlbumReferences) error {
	fs := i.VFS()
	bs := bufio.NewScanner(tr)

	for bs.Scan() {
		ref := Reference{}
		if err := json.Unmarshal(bs.Bytes(), &ref); err != nil {
			return err
		}

		file, err := fs.FileByPath(dstDoc.Fullpath + ref.Filepath)
		if err != nil {
			// XXX Ignore missing photos (we have this for migrating some cozy v2)
			continue
		}

		if docRef, ok := (*albums)[ref.Albumid]; ok {
			file.AddReferencedBy(docRef)
			if err = couchdb.UpdateDoc(i, file); err != nil {
				return err
			}
		}
	}

	return nil
}

func createFile(fs vfs.VFS, hdr *tar.Header, tr *tar.Reader, dstDoc *vfs.DirDoc) error {
	name := strings.TrimPrefix(hdr.Name, "files/")
	filename := path.Base(name)
	mime, class := vfs.ExtractMimeAndClassFromFilename(filename)
	now := time.Now()
	executable := hdr.FileInfo().Mode()&0100 != 0

	dirname := path.Join(dstDoc.Fullpath, path.Dir(name))
	dirDoc, err := fs.DirByPath(dirname)
	if err != nil {
		// XXX Tarball from cozy v2 exports can have files in a non-existant directory
		if dirDoc, err = vfs.MkdirAll(fs, dirname, nil); err != nil {
			return err
		}
	}
	fileDoc, err := vfs.NewFileDoc(filename, dirDoc.ID(), hdr.Size, nil, mime, class, now, executable, false, nil)
	if err != nil {
		return err
	}

	file, err := fs.CreateFile(fileDoc, nil)
	if err != nil {
		ext := path.Ext(fileDoc.DocName)
		fileName := fileDoc.DocName[0 : len(fileDoc.DocName)-len(ext)]
		fileDoc.DocName = fmt.Sprintf("%s-conflict-%d%s", fileName, rand.Int(), ext)
		file, err = fs.CreateFile(fileDoc, nil)
		if err != nil {
			return err
		}
	}

	_, err = io.Copy(file, tr)
	cerr := file.Close()
	if err != nil {
		return err
	}
	return cerr
}

// untar untar doc directory
func untar(r io.Reader, dst *vfs.DirDoc, instance *instance.Instance) error {
	fs := instance.VFS()

	// tar+gzip reader
	gr, err := gzip.NewReader(r)
	if err != nil {
		logger.WithDomain(instance.Domain).Errorf("Can't open gzip reader for import: %s", err)
		return err
	}
	defer gr.Close()
	tgz := tar.NewReader(gr)

	albumsRef := make(AlbumReferences)

	for {
		hdr, err := tgz.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.WithDomain(instance.Domain).Errorf("Error on import: %s", err)
			return err
		}

		parts := strings.SplitN(path.Clean(hdr.Name), "/", 2)
		var name, doctype string
		if len(parts) > 1 {
			doctype = parts[0]
			name = parts[1]
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if doctype == "files" {
				dirname := path.Join(dst.Fullpath, name)
				if _, err = vfs.MkdirAll(fs, dirname, nil); err != nil {
					logger.WithDomain(instance.Domain).Errorf("Can't import directory %s: %s", hdr.Name, err)
					return err
				}
			}

		case tar.TypeReg:
			if doctype == "albums" && name == albumsFile {
				err = createAlbums(instance, tgz, &albumsRef)
				if err != nil {
					logger.WithDomain(instance.Domain).Errorf("Can't import album %s: %s", hdr.Name, err)
					return err
				}
			} else if doctype == "albums" && name == referencesFile {
				err = fillAlbums(instance, tgz, dst, &albumsRef)
				if err != nil {
					logger.WithDomain(instance.Domain).Errorf("Can't import album %s: %s", hdr.Name, err)
					return err
				}
			} else if doctype == "contacts" {
				if err := createContact(fs, hdr, tgz, instance); err != nil {
					logger.WithDomain(instance.Domain).Errorf("Can't import contact %s: %s", hdr.Name, err)
					return err
				}
			} else if doctype == "files" {
				if err := createFile(fs, hdr, tgz, dst); err != nil {
					logger.WithDomain(instance.Domain).Errorf("Can't import file %s: %s", hdr.Name, err)
					return err
				}
			}

		default:
			logger.WithDomain(instance.Domain).Errorf("Unknown typeflag for import: %v", hdr.Typeflag)
			return errors.New("Unknown typeflag")
		}
	}

	return nil
}

// Import is used to import a tarball with files, photos, contacts to an instance
func Import(instance *instance.Instance, filename, destination string) error {
	r, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer r.Close()

	fs := instance.VFS()
	exist, err := vfs.DirExists(fs, destination)
	if err != nil {
		logger.WithDomain(instance.Domain).Errorf("Error for destination %s: %s", destination, err)
		return err
	}
	var dst *vfs.DirDoc
	if !exist {
		dst, err = vfs.Mkdir(fs, destination, nil)
		if err != nil {
			logger.WithDomain(instance.Domain).Errorf("Can't create destination directory %s: %s", destination, err)
			return err
		}
	} else {
		dst, err = fs.DirByPath(destination)
		if err != nil {
			logger.WithDomain(instance.Domain).Errorf("Can't find destination directory %s: %s", destination, err)
			return err
		}
	}

	return untar(r, dst, instance)
}
