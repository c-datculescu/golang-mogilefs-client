/*

Copyright 2015 Adrian Ulrich
Copyright 2015 Fixxpunkt AG

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

package mogilefs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
)

const (
	cmd_getpaths     = "get_paths"
	cmd_rename       = "rename"
	cmd_delete       = "delete"
	cmd_debug        = "file_debug"
	cmd_create_open  = "create_open"
	cmd_create_close = "create_close"
)

type countingReader struct {
	r      io.Reader
	nbytes int
}

func (cr *countingReader) Read(buffer []byte) (nr int, err error) {
	nr, err = cr.r.Read(buffer)
	cr.nbytes += nr
	return
}

/**
 * @desc Returns an established TCP connection to one of the specified trackers
 * @return conn net.Conn connection
 * @return err error last connection error if all trackers are down
 */
func (m *MogileFsClient) getTrackerConnection() (conn net.Conn, err error) {
	if m.isInitialized == false {
		for _, ignoreBlacklist := range [2]bool{false, true} {
			for _, host := range m.trackers {
				m.last_tracker = host

				if ignoreBlacklist == false && m.trackerIsBad(m.last_tracker) {
					continue
				}

				conn, err = net.DialTimeout("tcp", m.last_tracker, m.dial_timeout)
				if err == nil {
					m.localConn = conn
					m.isInitialized = true
					m.reconnectCounter = 0
					// we connected to this tracker for whatever reason: it is NOT whitelisted now - it will only be
					// whitelisted after returning a successful command or/and finishing the dead timeout
					return m.localConn, nil
				} else {
					m.markTrackerAsBad(m.last_tracker)
				}
			}
		}
	}

	return m.localConn, nil
}

// Destroy closes down the socket in a ordered fashion
func (m *MogileFsClient) Destroy() {
	m.localConn.Close()
}

/**
 * @desc Returns a tracker connection so it can be closed (or maybe put in a pool in a later version
 * @param conn net.Conn as handed out by getTrackerConnection()
 */
func (m *MogileFsClient) returnTrackerConnection(conn net.Conn, hadError bool) {
	if hadError == true {
		m.markTrackerAsBad(m.last_tracker)
	} else { // else: could keepalive
		m.markTrackerAsAlive(m.last_tracker)
	}
	//conn.Close()
}

/**
 * @desc Performs a request on the connected mogilefsd
 * @param command string the mogilefsd command to execute
 * @param args url.Values list of the arguments of 'command'
 * @return values url.Values of the result
 * @return err error returned by the tracker - nil on success
 */
var reMogileOk = regexp.MustCompile("^OK (.*)\r\n$")
var reMogileFail = regexp.MustCompile("^ERR (\\S+) ")

func (m *MogileFsClient) DoRequest(command string, args url.Values) (values url.Values, err error) {

	// change command into something understood by mogilefsd
	// format: COMMAND URLENCODED_ARGS\r\n
	command += " " + args.Encode() + "\r\n"

	tracker_reply := ""   // buffer to store the tracker reply
	blame_tracker := true // passed to returnTrackerConnection to mark a tracker as 'suspect'

	tracker_conn, tracker_conn_err := m.getTrackerConnection()
	err = tracker_conn_err
	if err == nil {
		_, err = tracker_conn.Write([]byte(command))
		if err == nil {
			b := bufio.NewReader(tracker_conn)
			tracker_reply, err = b.ReadString('\n')
		}
	}

	if len(tracker_reply) > 0 {
		okMatch := reMogileOk.FindAllStringSubmatch(tracker_reply, 1)
		if okMatch == nil {
			// reply was not ok: try to get a better error message
			failMatch := reMogileFail.FindAllStringSubmatch(tracker_reply, 1)
			if failMatch == nil {
				err = errors.New("internal:invalid tracker reply")
			} else {
				err = fmt.Errorf("mogilefsd:%s", failMatch[0][1])
				blame_tracker = false // that's not a tracker failure
			}
		} else {
			// reply was probably ok: just let
			// ParseQuery() decide the outcome of err
			values, err = url.ParseQuery(okMatch[0][1])
			blame_tracker = false
		}
	}

	m.returnTrackerConnection(tracker_conn, blame_tracker)

	return
}
