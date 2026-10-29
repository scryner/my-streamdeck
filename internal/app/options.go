package app

type RunOptions struct {
	EnablePprof                 bool
	Verbose                     bool
	caffeinateSessionDir        string
	caffeinateRestoreGeneration string
	caffeinateSaveGeneration    string
}
