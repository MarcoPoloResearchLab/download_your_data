package sqlite3

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>

static int go_sqlite_bind_text(sqlite3_stmt* statement, int parameter_index, const char* value, int value_length) {
    return sqlite3_bind_text(statement, parameter_index, value, value_length, SQLITE_TRANSIENT);
}

static int go_sqlite_bind_blob(sqlite3_stmt* statement, int parameter_index, const void* value, int value_length) {
    return sqlite3_bind_blob(statement, parameter_index, value, value_length, SQLITE_TRANSIENT);
}
*/
import "C"

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"time"
	"unsafe"
)

// DriverName is the canonical product-owned SQLite driver identity.
const DriverName = "download-your-data-sqlite3"

func init() {
	sql.Register(DriverName, &Driver{})
}

type Driver struct{}

func (sqliteDriver *Driver) Open(name string) (driver.Conn, error) {
	nameCString := C.CString(name)
	defer C.free(unsafe.Pointer(nameCString))

	var databaseHandle *C.sqlite3
	openFlags := C.int(C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX)
	resultCode := C.sqlite3_open_v2(nameCString, &databaseHandle, openFlags, nil)
	if resultCode != C.SQLITE_OK {
		errorMessage := "unknown SQLite open error"
		if databaseHandle != nil {
			errorMessage = C.GoString(C.sqlite3_errmsg(databaseHandle))
			C.sqlite3_close_v2(databaseHandle)
		}
		return nil, fmt.Errorf("open SQLite database: %s", errorMessage)
	}
	C.sqlite3_busy_timeout(databaseHandle, 30_000)
	return &Connection{databaseHandle: databaseHandle}, nil
}

type Connection struct {
	databaseHandle *C.sqlite3
	mutex          sync.Mutex
	closed         bool
}

func (connection *Connection) Prepare(query string) (driver.Stmt, error) {
	if connection.closed {
		return nil, driver.ErrBadConn
	}
	return &Statement{connection: connection, query: query}, nil
}

func (connection *Connection) Close() error {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	if connection.closed {
		return nil
	}
	resultCode := C.sqlite3_close_v2(connection.databaseHandle)
	if resultCode != C.SQLITE_OK {
		return connection.sqliteError(resultCode)
	}
	connection.closed = true
	connection.databaseHandle = nil
	return nil
}

func (connection *Connection) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}

func (connection *Connection) BeginTx(contextValue context.Context, options driver.TxOptions) (driver.Tx, error) {
	if options.ReadOnly {
		return nil, fmt.Errorf("read-only transactions are not supported")
	}
	if _, executeError := connection.ExecContext(contextValue, "BEGIN", nil); executeError != nil {
		return nil, executeError
	}
	return &Transaction{connection: connection}, nil
}

func (connection *Connection) Ping(contextValue context.Context) error {
	if contextError := contextValue.Err(); contextError != nil {
		return contextError
	}
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	if connection.closed {
		return driver.ErrBadConn
	}
	return nil
}

func (connection *Connection) ExecContext(contextValue context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	if contextError := contextValue.Err(); contextError != nil {
		return nil, contextError
	}
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	if connection.closed {
		return nil, driver.ErrBadConn
	}

	if len(arguments) == 0 {
		queryCString := C.CString(query)
		defer C.free(unsafe.Pointer(queryCString))
		var errorCString *C.char
		resultCode := C.sqlite3_exec(connection.databaseHandle, queryCString, nil, nil, &errorCString)
		if resultCode != C.SQLITE_OK {
			errorMessage := connection.errorMessage(resultCode)
			if errorCString != nil {
				errorMessage = C.GoString(errorCString)
				C.sqlite3_free(unsafe.Pointer(errorCString))
			}
			return nil, fmt.Errorf("SQLite execution failed: %s", errorMessage)
		}
		return Result{
			lastInsertID: int64(C.sqlite3_last_insert_rowid(connection.databaseHandle)),
			rowsAffected: int64(C.sqlite3_changes(connection.databaseHandle)),
		}, nil
	}

	statementHandle, prepareError := connection.prepareLocked(query)
	if prepareError != nil {
		return nil, prepareError
	}
	defer C.sqlite3_finalize(statementHandle)
	if bindError := connection.bindArguments(statementHandle, arguments); bindError != nil {
		return nil, bindError
	}
	for {
		resultCode := C.sqlite3_step(statementHandle)
		if resultCode == C.SQLITE_DONE {
			break
		}
		if resultCode == C.SQLITE_ROW {
			continue
		}
		return nil, connection.sqliteError(resultCode)
	}
	return Result{
		lastInsertID: int64(C.sqlite3_last_insert_rowid(connection.databaseHandle)),
		rowsAffected: int64(C.sqlite3_changes(connection.databaseHandle)),
	}, nil
}

func (connection *Connection) QueryContext(contextValue context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	if contextError := contextValue.Err(); contextError != nil {
		return nil, contextError
	}
	connection.mutex.Lock()
	if connection.closed {
		connection.mutex.Unlock()
		return nil, driver.ErrBadConn
	}

	statementHandle, prepareError := connection.prepareLocked(query)
	if prepareError != nil {
		connection.mutex.Unlock()
		return nil, prepareError
	}
	if bindError := connection.bindArguments(statementHandle, arguments); bindError != nil {
		C.sqlite3_finalize(statementHandle)
		connection.mutex.Unlock()
		return nil, bindError
	}

	columnCount := int(C.sqlite3_column_count(statementHandle))
	columns := make([]string, columnCount)
	for columnIndex := 0; columnIndex < columnCount; columnIndex++ {
		columns[columnIndex] = C.GoString(C.sqlite3_column_name(statementHandle, C.int(columnIndex)))
	}
	return &Rows{
		connection:      connection,
		statementHandle: statementHandle,
		columns:         columns,
		lockHeld:        true,
	}, nil
}

func (connection *Connection) CheckNamedValue(namedValue *driver.NamedValue) error {
	switch namedValue.Value.(type) {
	case nil, int64, float64, bool, []byte, string, time.Time:
		return nil
	case int:
		namedValue.Value = int64(namedValue.Value.(int))
		return nil
	case int32:
		namedValue.Value = int64(namedValue.Value.(int32))
		return nil
	case uint:
		namedValue.Value = int64(namedValue.Value.(uint))
		return nil
	default:
		return driver.ErrSkip
	}
}

func (connection *Connection) prepareLocked(query string) (*C.sqlite3_stmt, error) {
	queryCString := C.CString(query)
	defer C.free(unsafe.Pointer(queryCString))
	var statementHandle *C.sqlite3_stmt
	resultCode := C.sqlite3_prepare_v2(connection.databaseHandle, queryCString, -1, &statementHandle, nil)
	if resultCode != C.SQLITE_OK {
		return nil, connection.sqliteError(resultCode)
	}
	return statementHandle, nil
}

func (connection *Connection) bindArguments(statementHandle *C.sqlite3_stmt, arguments []driver.NamedValue) error {
	parameterCount := int(C.sqlite3_bind_parameter_count(statementHandle))
	if parameterCount != len(arguments) {
		return fmt.Errorf("SQLite expected %d parameters but received %d", parameterCount, len(arguments))
	}
	for argumentIndex, argument := range arguments {
		parameterIndex := C.int(argumentIndex + 1)
		var resultCode C.int
		switch typedValue := argument.Value.(type) {
		case nil:
			resultCode = C.sqlite3_bind_null(statementHandle, parameterIndex)
		case int64:
			resultCode = C.sqlite3_bind_int64(statementHandle, parameterIndex, C.sqlite3_int64(typedValue))
		case float64:
			resultCode = C.sqlite3_bind_double(statementHandle, parameterIndex, C.double(typedValue))
		case bool:
			integerValue := C.sqlite3_int64(0)
			if typedValue {
				integerValue = 1
			}
			resultCode = C.sqlite3_bind_int64(statementHandle, parameterIndex, integerValue)
		case string:
			valueCString := C.CString(typedValue)
			resultCode = C.go_sqlite_bind_text(statementHandle, parameterIndex, valueCString, C.int(len(typedValue)))
			C.free(unsafe.Pointer(valueCString))
		case []byte:
			if len(typedValue) == 0 {
				resultCode = C.go_sqlite_bind_blob(statementHandle, parameterIndex, nil, 0)
			} else {
				resultCode = C.go_sqlite_bind_blob(statementHandle, parameterIndex, unsafe.Pointer(&typedValue[0]), C.int(len(typedValue)))
			}
		case time.Time:
			formattedValue := typedValue.UTC().Format(time.RFC3339Nano)
			valueCString := C.CString(formattedValue)
			resultCode = C.go_sqlite_bind_text(statementHandle, parameterIndex, valueCString, C.int(len(formattedValue)))
			C.free(unsafe.Pointer(valueCString))
		default:
			return fmt.Errorf("unsupported SQLite parameter type %T", argument.Value)
		}
		if resultCode != C.SQLITE_OK {
			return connection.sqliteError(resultCode)
		}
	}
	return nil
}

func (connection *Connection) sqliteError(resultCode C.int) error {
	return fmt.Errorf("SQLite error %d: %s", int(resultCode), connection.errorMessage(resultCode))
}

func (connection *Connection) errorMessage(resultCode C.int) string {
	if connection.databaseHandle == nil {
		return C.GoString(C.sqlite3_errstr(resultCode))
	}
	return C.GoString(C.sqlite3_errmsg(connection.databaseHandle))
}

type Statement struct {
	connection *Connection
	query      string
}

func (statement *Statement) Close() error {
	return nil
}

func (statement *Statement) NumInput() int {
	return -1
}

func (statement *Statement) Exec(arguments []driver.Value) (driver.Result, error) {
	namedArguments := make([]driver.NamedValue, len(arguments))
	for argumentIndex, argument := range arguments {
		namedArguments[argumentIndex] = driver.NamedValue{Ordinal: argumentIndex + 1, Value: argument}
	}
	return statement.connection.ExecContext(context.Background(), statement.query, namedArguments)
}

func (statement *Statement) Query(arguments []driver.Value) (driver.Rows, error) {
	namedArguments := make([]driver.NamedValue, len(arguments))
	for argumentIndex, argument := range arguments {
		namedArguments[argumentIndex] = driver.NamedValue{Ordinal: argumentIndex + 1, Value: argument}
	}
	return statement.connection.QueryContext(context.Background(), statement.query, namedArguments)
}

type Transaction struct {
	connection *Connection
}

func (transaction *Transaction) Commit() error {
	_, executeError := transaction.connection.ExecContext(context.Background(), "COMMIT", nil)
	return executeError
}

func (transaction *Transaction) Rollback() error {
	_, executeError := transaction.connection.ExecContext(context.Background(), "ROLLBACK", nil)
	return executeError
}

type Rows struct {
	connection      *Connection
	statementHandle *C.sqlite3_stmt
	columns         []string
	closed          bool
	lockHeld        bool
}

func (rows *Rows) Columns() []string {
	return rows.columns
}

func (rows *Rows) Close() error {
	if rows.closed {
		return nil
	}
	rows.closed = true
	if rows.statementHandle != nil {
		C.sqlite3_finalize(rows.statementHandle)
		rows.statementHandle = nil
	}
	if rows.lockHeld {
		rows.lockHeld = false
		rows.connection.mutex.Unlock()
	}
	return nil
}

func (rows *Rows) Next(destination []driver.Value) error {
	if rows.closed {
		return io.EOF
	}
	resultCode := C.sqlite3_step(rows.statementHandle)
	if resultCode == C.SQLITE_DONE {
		rows.Close()
		return io.EOF
	}
	if resultCode != C.SQLITE_ROW {
		errorValue := rows.connection.sqliteError(resultCode)
		rows.Close()
		return errorValue
	}
	for columnIndex := range destination {
		columnType := C.sqlite3_column_type(rows.statementHandle, C.int(columnIndex))
		switch columnType {
		case C.SQLITE_INTEGER:
			destination[columnIndex] = int64(C.sqlite3_column_int64(rows.statementHandle, C.int(columnIndex)))
		case C.SQLITE_FLOAT:
			destination[columnIndex] = float64(C.sqlite3_column_double(rows.statementHandle, C.int(columnIndex)))
		case C.SQLITE_TEXT:
			textPointer := C.sqlite3_column_text(rows.statementHandle, C.int(columnIndex))
			textLength := C.sqlite3_column_bytes(rows.statementHandle, C.int(columnIndex))
			destination[columnIndex] = C.GoStringN((*C.char)(unsafe.Pointer(textPointer)), textLength)
		case C.SQLITE_BLOB:
			blobPointer := C.sqlite3_column_blob(rows.statementHandle, C.int(columnIndex))
			blobLength := C.sqlite3_column_bytes(rows.statementHandle, C.int(columnIndex))
			if blobLength == 0 {
				destination[columnIndex] = []byte{}
			} else {
				destination[columnIndex] = C.GoBytes(blobPointer, blobLength)
			}
		case C.SQLITE_NULL:
			destination[columnIndex] = nil
		default:
			destination[columnIndex] = nil
		}
	}
	return nil
}

type Result struct {
	lastInsertID int64
	rowsAffected int64
}

func (result Result) LastInsertId() (int64, error) {
	return result.lastInsertID, nil
}

func (result Result) RowsAffected() (int64, error) {
	return result.rowsAffected, nil
}
