package types

// derefHTTPActions dereferences HTTP-related actions
func derefHTTPActions(a TypedAction) (TypedAction, bool) {
	switch v := a.(type) {
	case *HTTPAction:
		return *v, true
	case *GraphQLQueryAction:
		return *v, true
	case *WSConnectAction:
		return *v, true
	case *WSSendAction:
		return *v, true
	}
	return nil, false
}

// derefFileActions dereferences file operation actions
func derefFileActions(a TypedAction) (TypedAction, bool) {
	switch v := a.(type) {
	case *FileReadAction:
		return *v, true
	case *FileWriteAction:
		return *v, true
	case *FileExistsAction:
		return *v, true
	case *FileGlobAction:
		return *v, true
	}
	return nil, false
}

// derefCodeActions dereferences code analysis actions
func derefCodeActions(a TypedAction) (TypedAction, bool) {
	switch v := a.(type) {
	case *CodeAnalyzeAction:
		return *v, true
	case *CodeLintAction:
		return *v, true
	case *CodeSymbolsAction:
		return *v, true
	}
	return nil, false
}

// derefBrowserActions dereferences browser automation actions
func derefBrowserActions(a TypedAction) (TypedAction, bool) {
	switch v := a.(type) {
	case *BrowserGotoAction:
		return *v, true
	case *BrowserClickAction:
		return *v, true
	case *BrowserFillAction:
		return *v, true
	case *BrowserEvalAction:
		return *v, true
	}
	return nil, false
}

// derefProcessDBActions dereferences process and database actions
func derefProcessDBActions(a TypedAction) (TypedAction, bool) {
	switch v := a.(type) {
	case *ProcessExecAction:
		return *v, true
	case *DBQueryAction:
		return *v, true
	case *DBAssertAction:
		return *v, true
	}
	return nil, false
}

// derefOtherActions dereferences remaining action types
func derefOtherActions(a TypedAction) (TypedAction, bool) {
	switch v := a.(type) {
	case *NavigateAction:
		return *v, true
	case *WaitAction:
		return *v, true
	case *MCPCallAction:
		return *v, true
	}
	return nil, false
}
