// Package taskresult holds the record of what one maintenance task did, shared by the maintenance runner (which produces results in the app process) and the elevate package (which carries results back from the elevated child process). It exists as its own package so that elevate does not import maintenance and maintenance can import elevate without a cycle.
//
// The JSON form of Result is the wire format between the app and its elevated child (see package elevate). Renaming a field or an Outcome value is a breaking change to it.
package taskresult
