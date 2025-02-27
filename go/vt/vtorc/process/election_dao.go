/*
   Copyright 2015 Shlomi Noach, courtesy Booking.com

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package process

import (
	"github.com/openark/golib/sqlutils"

	"vitess.io/vitess/go/vt/log"
	"vitess.io/vitess/go/vt/vtorc/config"
	"vitess.io/vitess/go/vt/vtorc/db"
	"vitess.io/vitess/go/vt/vtorc/util"
)

// AttemptElection tries to grab leadership (become active node)
func AttemptElection() (bool, error) {
	{
		sqlResult, err := db.ExecVTOrc(`
		insert ignore into active_node (
				anchor, hostname, token, first_seen_active, last_seen_active
			) values (
				1, ?, ?, now(), now()
			)
		`,
			ThisHostname, util.ProcessToken.Hash,
		)
		if err != nil {
			log.Error(err)
			return false, err
		}
		rows, err := sqlResult.RowsAffected()
		if err != nil {
			log.Error(err)
			return false, err
		}
		if rows > 0 {
			// We managed to insert a row
			return true, nil
		}
	}
	{
		// takeover from a node that has been inactive
		sqlResult, err := db.ExecVTOrc(`
			update active_node set
				hostname = ?,
				token = ?,
				first_seen_active=now(),
				last_seen_active=now()
			where
				anchor = 1
			  and last_seen_active < (now() - interval ? second)
		`,
			ThisHostname, util.ProcessToken.Hash, config.ActiveNodeExpireSeconds,
		)
		if err != nil {
			log.Error(err)
			return false, err
		}
		rows, err := sqlResult.RowsAffected()
		if err != nil {
			log.Error(err)
			return false, err
		}
		if rows > 0 {
			// We managed to update a row: overtaking a previous leader
			return true, nil
		}
	}
	{
		// Update last_seen_active is this very node is already the active node
		sqlResult, err := db.ExecVTOrc(`
			update active_node set
				last_seen_active=now()
			where
				anchor = 1
				and hostname = ?
				and token = ?
		`,
			ThisHostname, util.ProcessToken.Hash,
		)
		if err != nil {
			log.Error(err)
			return false, err
		}
		rows, err := sqlResult.RowsAffected()
		if err != nil {
			log.Error(err)
			return false, err
		}
		if rows > 0 {
			// Reaffirmed our own leadership
			return true, nil
		}
	}
	return false, nil
}

// GrabElection forcibly grabs leadership. Use with care!!
func GrabElection() error {
	_, err := db.ExecVTOrc(`
			replace into active_node (
					anchor, hostname, token, first_seen_active, last_seen_active
				) values (
					1, ?, ?, now(), now()
				)
			`,
		ThisHostname, util.ProcessToken.Hash,
	)
	if err != nil {
		log.Error(err)
	}
	return err
}

// Reelect clears the way for re-elections. Active node is immediately demoted.
func Reelect() error {
	_, err := db.ExecVTOrc(`delete from active_node where anchor = 1`)
	if err != nil {
		log.Error(err)
	}
	return err
}

// ElectedNode returns the details of the elected node, as well as answering the question "is this process the elected one"?
func ElectedNode() (node *NodeHealth, isElected bool, err error) {
	node = &NodeHealth{}
	query := `
		select
			hostname,
			token,
			first_seen_active,
			last_seen_Active
		from
			active_node
		where
			anchor = 1
		`
	err = db.QueryVTOrcRowsMap(query, func(m sqlutils.RowMap) error {
		node.Hostname = m.GetString("hostname")
		node.Token = m.GetString("token")
		node.FirstSeenActive = m.GetString("first_seen_active")
		node.LastSeenActive = m.GetString("last_seen_active")

		return nil
	})

	isElected = (node.Hostname == ThisHostname && node.Token == util.ProcessToken.Hash)
	if err != nil {
		log.Error(err)
	}
	return node, isElected, err //nolint copylocks: return copies lock value
}
