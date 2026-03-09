/*
|--------------------------------------------------------------------------
| Drive Service
|--------------------------------------------------------------------------
|
| Package-level helpers for accessing the Drive service. Use after the
| Drive plugin has been registered and the app has booted.
|
|   disk, err := drive.Use("")     // default disk
|   disk, err := drive.Use("s3")   // named disk
|   disk.Put("key", reader)
|   url, _ := disk.GetUrl("key")
|
*/

package drive

// Use returns the disk with the given name. Empty name uses the default disk.
// Returns nil, err if Drive plugin is not registered or disk not found.
func Use(name string) (Disk, error) {
	m := GetGlobal()
	if m == nil {
		return nil, ErrDriveNotRegistered
	}
	return m.Use(name)
}

// UseDefault returns the default disk (same as Use("")).
func UseDefault() (Disk, error) {
	return Use("")
}
