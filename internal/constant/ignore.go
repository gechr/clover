package constant

// clover:ignore control comments suppress directive scanning. The bare form
// ignores the next line; the -start/-end pair brackets a block; -file ignores
// the whole file. They are full keyword tokens so the scanner matches them the
// same way it finds any directive.
const (
	Ignore      = DirectiveKeyword + "ignore"       // clover:ignore
	IgnoreEnd   = DirectiveKeyword + "ignore-end"   // clover:ignore-end
	IgnoreFile  = DirectiveKeyword + "ignore-file"  // clover:ignore-file
	IgnoreStart = DirectiveKeyword + "ignore-start" // clover:ignore-start
)
