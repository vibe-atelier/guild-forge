package guildstore

import "nexus/internal/proto"

// Roles are kept in roles.json. A member's roles
// are listed in members.json (Member.Roles).

const rolesFileName = "roles.json"

// LoadRoles reads roles.json. A guild without one gets the Everyone role
// with its default permissions.
func LoadRoles(dataDir string) ([]proto.Role, error) {
	var r []proto.Role
	if err := loadJSONFile(dataDir, rolesFileName, &r); err != nil {
		return nil, err
	}
	for _, x := range r {
		if x.ID == proto.EveryoneRole {
			return r, nil
		}
	}
	every := proto.Role{ID: proto.EveryoneRole, Name: "Everyone", Perms: append([]string{}, proto.EveryoneDefaults...)}
	return append([]proto.Role{every}, r...), nil
}

// SaveRoles writes roles.json.
func SaveRoles(dataDir string, r []proto.Role) error {
	return saveJSONFile(dataDir, rolesFileName, r)
}
