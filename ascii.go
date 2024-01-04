package main

// To Do: The only issue I currently see is that sometimes a character is snipped off the end of a string.
// Which can cost the entire string if it wasn't long enough, so that's happening before the length check.
// Found in UTF-16 mode.

import (
	"flag"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Sample CMD I used:
// del "D:\WinSoft\Docs\OneNote.backups\16.0\Backup\*.one.txt" /s
// D:\Dev\Projects\ASCII\ASCII\bin\Release\net6 .0\publish\win - x64\ASCII.exe - i "D:\WinSoft\Docs\OneNote.backups\16.0\Backup\Main Notebook\*.one"--min - len 6--skip - older - match "(" - o - d--alpha - ratio 85--utf16--utf8
var description = `
Extracts text from binary, including optionally UTF-8/16, with controls and output options.

ASCII mode grabs lower-bit characters.  A few control characters and 0x20 - 0x7E.
UTF8 mode accepts those and recognizes possible UTF-8, using the encoding standard.

Inspired by the 1985-86 ASCII.exe program from SEA (System Enhancement Associates) of ARC (pre-PKZIP fame), 
by Thom Henderson, one of the early heroes of the pre-Internet.

Yes, I still have a copy of ASCII.exe.  It runs fine in DOSBox-X.

Simple usage: ascii <input file>
This will extract standard ASCII, no UTF-8/16, that is six characters or longer, and output it to the console.

`

/*
 The character length is encoded in the first byte, top nibble.  As in high-bit on, the count of other high-bits descending is number of bytes following.  If 0, it IS a following byte.
 The first five bits are part of the unicode character.
     110x xxxx   One more byte follows
     1110 xxxx   Two more bytes follow
     1111 0xxx   Three more bytes follow
     10xx xxxx   A continuation of one of the multi-byte characters.  (i.e. allows you to know that this byte is part of a sequence.)

 e.g. Multi-Byte chars can be of one of these formats:
     110xxxxx 10xxxxxx
     1110xxxx 10xxxxxx 10xxxxxx
     11110xxx 10xxxxxx 10xxxxxx 10xxxxxx
 Unicode: https://www.rfc-editor.org/rfc/rfc3629
 The UTF16 searcher only looked for ASCII characters.
*/

var (
	baseNames   []string
	debugOutput = false
	writeFiles  = false
	writePath   = ""
	writeStdOut = false
	startPath   = ""
	// Used for file masks.
	// Debug Mode Data - used for extra-verbose output.
	includedFileNames []string
	excludedFileNames []string
	stringCount       = 0
	utf16StringCount  = 0
)

func IIF(cond bool, str1 string, str2 string) string {
	if cond {
		return str1
	}
	return str2
}

func PrintHelp() {
	fmt.Fprintf(os.Stderr, description)
	flag.PrintDefaults()
}

type SORTBY int

const SORTBY_NONE SORTBY = 0
const SORTBY_NAME SORTBY = 1
const SORTBY_DATE SORTBY = 2

// For directory recursion, need the current directory and list of subdirs in it, and to go through them.
// subdirs and files are the names within target, not the full path.
// DOES NOT SUPPORT FILEMASK YET
func filesInDirectory(target string, filemask string, sortby SORTBY, ascending bool) (subdirs []string, files []string) {
	var filelist []fs.DirEntry

	pFile, err := os.Open(target)
	if err == nil {
		defer pFile.Close()
		filelist, err = pFile.ReadDir(0)
	}

	if sortby == SORTBY_DATE {
		if ascending {
			sort.Slice(filelist, func(i, j int) bool {
				fileI, _ := filelist[i].Info()
				fileJ, _ := filelist[j].Info()
				return fileI.ModTime().Before(fileJ.ModTime())
			})
		} else {
			sort.Slice(filelist, func(i, j int) bool {
				fileI, _ := filelist[i].Info()
				fileJ, _ := filelist[j].Info()
				return fileJ.ModTime().Before(fileI.ModTime())
			})
		}
	} else { // Name sort
		if ascending {
			sort.Slice(filelist, func(i, j int) bool {
				return filelist[i].Name() < filelist[j].Name()
			})
		} else if sortby == SORTBY_NAME {
			sort.Slice(filelist, func(i, j int) bool {
				return filelist[i].Name() > filelist[j].Name()
			})
		}
	}

	// Iterate through all files, matching and then sort
	if err == nil {
		for _, f := range filelist {
			if f.IsDir() {
				f.Info()
				subdirs = append(subdirs, f.Name())
			} else {
				if len(filemask) > 0 {
					res, err := filepath.Match(filemask, f.Name())
					if err == nil && res {
						files = append(files, f.Name())
					}
				} else {
					files = append(files, f.Name())
				}
			}
		}
	}
	return subdirs, files
}

func main() {
	var pInputFilename = flag.String("i", "", "Filename or mask to parse.  Assumes CWD.")
	var pMinLen = flag.Int("min-len", 6, "Minimum length of ASCII characters to grab. ")
	var pWriteOutput = flag.Bool("o", false, "Should files, less extension plus .txt, be written?  Default: False")
	var pWriteOutputPath = flag.String("p", "", "Path to write output to, if different from source.  Implies flattening from dir1/dir2/filename to dir1-dir2-filename.")
	var putf8 = flag.Bool("utf8", false, "Include value UTF8 characters.  (Default is pure lower-bit ASCII.)\nWarning: Lots of junk looks like UTF-8.  Non-UTF8 is usually cleaner.")
	var putf16 = flag.Bool("utf16", false, "Look for UTF-16 (BE) strings.  Only handles ASCII-ish ones.")
	var pSkipOlderMatch = flag.String("skip-older-match", "", "For files with dates/incrementors in their names, allows grabbing the newest that don't differ until this string.\n(e.g. notes_20220212.txt, notes_20211220.txt, pass '_' and only the newest is processed.  Case-Sensitive.)\nPer Directory.")
	var pAlphaRatio = flag.Int("alpha-ratio", 0, "Percentage required alphanumeric+,. in a string.  Default is 0 - no requirement.  80 should reduce noise.")
	var pRecurseDirs = flag.Bool("r", false, "Recurse Directories")
	var pVerbose = flag.Bool("v", false, "Writes the output to stdout.  This is always on if not writing files, but defaults off otherwise.")
	var pDebug = flag.Bool("d", false, "Debug - output directories and filenames, and stats.")

	flag.Usage = func() {
		PrintHelp()
	}
	flag.Parse()
	InputFileName := *pInputFilename
	minLen := *pMinLen
	filesProcessed := 0 // Does not count those skipped, e.g. redundants.
	minStr := 6
	debugOutput = *pDebug
	writeFiles = *pWriteOutput
	writePath = *pWriteOutputPath
	writeStdOut = *pVerbose
	// DirectoryInfo folder = new DirectoryInfo(Directory.GetCurrentDirectory());
	folder, _ := os.Getwd()
	fileName := *pInputFilename
	if minLen > 1 {
		minStr = minLen
	}

	if len(InputFileName) == 0 {
		if debugOutput {
			fmt.Printf("i = %s, minLen = %d, o = %s\n", InputFileName, minLen, writeFiles)
		}
		fmt.Printf("Error: input is required.  (The program can't do much without it.)\n")
		PrintHelp()
		return
	}
	if strings.Contains(InputFileName, string(os.PathSeparator)) { // Replace CWD with specified.
		pathName := filepath.Dir(InputFileName)
		if len(pathName) == 0 {
			fmt.Printf("Error: Invalid input path.\n")
			return
		}
		folder = pathName
		fileName = filepath.Base(InputFileName)
	} else {
		folder, _ = os.Getwd()
	}
	startPath = folder
	if debugOutput {
		fmt.Printf("Directory: %s", folder)
	}
	_, err := os.Stat(folder)
	if err != nil {
		fmt.Printf("ASCII Error: CWD not found! %s\n", err.Error())
		return
	}
	// Determine if we have a file mask
	_, err = os.Stat(filepath.Join(folder, fileName))
	if err != nil {
		// Test as a glob matcher.
		files, err := filepath.Glob(filepath.Join(folder, fileName))
		if err != nil {
			fmt.Printf("Bad filename: Could not find nor use as a pattern %s in %s.\n", fileName, InputFileName)
			return
		}
		if len(files) == 0 && !*pRecurseDirs {
			fmt.Printf("No files matching %s in %s found.\n", fileName, folder)
			return
		}
	}

	filesProcessed = RecurseDirectories(folder, *pRecurseDirs, fileName, minStr, *putf8, *putf16, *pAlphaRatio, *pSkipOlderMatch)
	if debugOutput {

		fmt.Printf("Processed %d files:\n", filesProcessed)
		for _, fname := range includedFileNames {
			fmt.Println(fname)
		}
		fmt.Println("Excluded Files:")
		for _, fname := range excludedFileNames {
			fmt.Println(fname)
		}

		fmt.Printf("Found %d ASCII/UTF-8 strings and %d UTF-16 strings.", stringCount, utf16StringCount)
	}
}

// / <summary>Recurse through directories to process (ASCII-fy) all files.</summary>
// / <param name="folder">Starting point</param>
// / <param name="recurse">Keep going down?  Command line parameter.</param>
// / <param name="fileMask">File matching mask</param>
// / <param name="minLen">Pass-Through: Minimum length of ASCII to catch.</param>
// / <param name="utf8">Pass-Through: Find UTF-8 Strings?</param>
// / <param name="utf16">Pass-Through: Find UTF-16 Strings?</param>
// / <param name="alphaRatio">Pass-Through, default 0.  How much of string must be alpha rather than punctuation/numeric?</param>
// / <param name="skipOlderMatch">Pass-Through, used to only grab newest matching file.</param>
// / <returns></returns>
func RecurseDirectories(folder string, recurse bool, fileMask string, minLen int, utf8 bool, utf16 bool, alphaRatio int, skipOlderMatch string) int {
	dirCount := 1
	dirs, files := filesInDirectory(folder, fileMask, SORTBY_DATE, false)

	baseNames = nil // File matching is per-directory
	for _, file := range files {
		AsciifyFile(folder, file, minLen, utf8, alphaRatio, skipOlderMatch, utf16)
	}

	if recurse {
		for _, dir := range dirs {
			dirCount += RecurseDirectories(filepath.Join(folder, dir), recurse, fileMask, minLen, utf8, utf16, alphaRatio, skipOlderMatch)
		}
	}

	return dirCount
}

// / <summary>
// / Extract ASCII or UTF8 data from one file.
// / </summary>
// / <returns>Was a file processed?  (False if it couldn't be opened or was redundant.)</returns>
func AsciifyFile(folder string, file string, minimumMatchLength int, utf8Mode bool, alphaRatio int, oldMatchString string, utf16Mode bool) bool {
	SepChar := "\n"
	fullFileName := filepath.Join(folder, file)
	if !PassesFileMatch(file, oldMatchString) { // Have we seen this basefile before?
		excludedFileNames = append(excludedFileNames, fullFileName)
		return false
	}
	includedFileNames = append(includedFileNames, fullFileName)
	if debugOutput {
		fmt.Printf("File: %s in Folder: %s\n", file, folder)
	}

	fileContents, err := os.ReadFile(fullFileName)
	if err != nil {
		fmt.Printf("ERROR: %s / %s: %s\n", folder, file, err.Error())
		return false
	}
	workString := ""
	resultString := ""

	fileIndex := 0
	isCharacterValid := false
	newChar := ""

	for fileIndex < len(fileContents) {
		// Try UTF16 first, if enabled, because safer on the index.  This call checks for minLen.
		if utf16Mode {
			var newIndex int // if the character is valid.
			isCharacterValid, newChar, newIndex = GetUTF16String(fileContents, fileIndex, minimumMatchLength)
			if isCharacterValid {
				// We should seldom have a workString already.  Log it if we do.  (Yes, double-vetting, but diff. lengths.)
				fileIndex = newIndex
				if VetString(workString, int(math.Min(3.0, float64(minimumMatchLength))), alphaRatio) {
					workString = strings.TrimSpace(workString)
					fmt.Printf("ASCII Error: UTF-16 found appending ASCII.\nASCII: %ss\nUTF-16: %s\n", workString, newChar)
					workString += SepChar + newChar
				} else {
					workString = newChar
				}
				if VetString(workString, minimumMatchLength, alphaRatio) {
					resultString += workString + SepChar
					utf16StringCount++
				}
				workString = ""
			}
		}

		isCharacterValid, newChar, fileIndex = GetChar(fileContents, fileIndex, utf8Mode)
		if isCharacterValid {
			workString += newChar
		} else { // Invalid - but check to see if we should write str
			if VetString(workString, minimumMatchLength, alphaRatio) {
				resultString += workString + SepChar
				stringCount++
			}
			workString = ""
		}
	}
	if VetString(workString, minimumMatchLength, alphaRatio) {
		resultString += workString + SepChar
		stringCount++
	}
	workString = ""

	// string ascii = System.Text.ASCIIEncoding.ASCII.GetString(dest.ToArray<byte>());
	if (!writeFiles) || (writeStdOut) {
		fmt.Println(resultString)
	}

	if writeFiles {
		if len(writePath) == 0 {
			err = os.WriteFile(filepath.Join(folder, file)+".txt", []byte(resultString), 0644)
			if err != nil {
				fmt.Printf("File Write Error to %s - %s: %s\n", folder, file, err.Error())
			}
		} else { //  Write to the specified path, flattened.
			// Len + 1 to trim off Path.DirectorySeparatorChar from file name.
			// TO DO: This should include the entire path from the starting location, with sep replaced.
			// That is folder - startPath
			newFileName := folder[len(startPath)+1:]
			newFileName = strings.ReplaceAll(newFileName, string(filepath.Separator), "-") + "-" + file + ".txt"
			if debugOutput {
				fmt.Printf("Writing %s to %s.\n", writePath, newFileName)
			}
			err = os.WriteFile(filepath.Join(writePath, newFileName), []byte(resultString), 0644)

			if err != nil {
				fmt.Printf("File Write Error to %s - %s: %s\n", folder, file, err.Error())
			}
		}
	}
	return true
}

// C# inline function equivalent
func isASCII(b byte) bool {
	return (((b > 31) && (b < 127)) || (b == 9) || (b == 10))
}

// / <summary>
// / Validates whether this string is acceptable: Is it long enough, and is it ASCII-enough.
// / For the latter, counts alphanumeric, space, CR/LF, period and comma.
// / </summary>
// / <param name="src"></param>
// / <param name="minLen"></param>
// / <param name="minRatio"></param>
// / <returns></returns>
func VetString(src string, minLen int, minRatio int) bool {
	if len(src) < minLen {
		return false
	}
	if minRatio > 0 { //  Count chars
		asciiChars := 0
		UTF8Chars := 0
		srcBytes := []byte(src)
		for _, c := range srcBytes {
			asc := int(c)
			// Counting alpha-numeric-common punctuations.  Excludes math, parens, etc.
			// 32: space; 44: comma; 46: period; 10: nl; 13: cr; 48-57: digits.
			if (asc == 32) || (asc == 44) || (asc == 46) || (asc == 10) || (asc == 13) || ((asc >= 48) && (asc <= 57)) ||
				((asc >= 65) && (asc <= 90)) || ((asc >= 97) && (asc <= 122)) {
				asciiChars++
			} else {
				UTF8Chars++ //  Won't really be used.
			}
		}
		if asciiChars*100/len(src) < minRatio {
			return false
		}
	}
	return true
}

// UnicodeCategory returns the Unicode Character Category of the given rune.
func UnicodeCategory(r rune) string {
	for name, table := range unicode.Categories {
		if len(name) == 2 && unicode.Is(table, r) {
			return name
		}
	}
	return "Cn"
}

// / <summary>
// / Finds the next single character - either 1, 2, 3 or 4 bytes.
// / </summary>
// / <param name="src">Bytes to extract chars from.</param>
// / <param name="startIndex">Current byte point, which is either incremented by 1 or by the length of the UTF8 character.</param>
// / <param name="UTF8">If False, only look at select ASCII.  Otherwise, parse up to four bytes.</param>
// / <returns>Empty string if this byte is not a character.  Otherwise, the character.  Return int is new index.</returns>
func GetChar(src []byte, startIndex int, UTF8 bool) (bool, string, int) {
	originalStartIndex := startIndex // Only used on failure-ish.
	additionalLength := 0
	if startIndex == len(src) {
		return false, "", startIndex
	}
	chars := ""
	b := src[startIndex]
	startIndex++ // ASCII always increments counter by 1.
	// This check is true for first character always.
	if isASCII(b) { // (((b > 31) && (b < 127)) || (b == 9) || (b == 10))   // Valid character
		chars += string(b)
		return true, chars, startIndex
	}
	// Not valid ASCII.
	if (!UTF8) || (b < 127) {
		return false, chars, startIndex //  Empty string.
	}
	// Check b for valid multi-byte UTF8 start.
	if (b | 0b01000000) == 0 { // If 0x10xxxxxx, not valid because it's a continuer.)
		return false, chars, startIndex
	}
	// Count the pattern, on-bits for data, zero for stop-count. Up to four on plus the off.
	for bit := 6; bit >= 3; bit-- {
		if ((b >> bit) & 1) == 1 {
			additionalLength++
		} else {
			break
		}
	}
	if (additionalLength < 1) || (additionalLength > 3) { // not valid, at least for us.
		return false, chars, startIndex
	}
	// Each of the following bytes (remember, incremented startIndex already) should start 0b10.
	var str []byte
	str = append(str, b)
	for index := 0; index < additionalLength; index++ {
		if len(src) < startIndex+index+1 {
			return false, chars, startIndex
		}
		if (src[startIndex+index] & 0b11000000) != 0b10000000 {
			return false, chars, startIndex
		}
		str = append(str, src[startIndex+index])
	}
	// Made it through
	startIndex += additionalLength
	r, _ := utf8.DecodeRune(str)
	if r == utf8.RuneError { // Empty or invalid
		startIndex = originalStartIndex + 1
		return false, chars, startIndex
	}
	// Determine if it's a valid Unicode for ASCII - a likely-desired character
	cat := UnicodeCategory(r)
	// C* is control, incl format, private
	if strings.HasPrefix(cat, "_C") { // unicode.Other, but that's *RangeTable type
		// var uc = char.GetUnicodeCategory(chars, 0)
		//  if (uc == UnicodeCategory.Surrogate) || (uc == UnicodeCategory.OtherNotAssigned) || (uc == UnicodeCategory.PrivateUse) || (uc == UnicodeCategory.Control) || (uc == UnicodeCategory.Format) {
		startIndex = originalStartIndex + 1
		return false, chars, startIndex
	}
	chars = string(r)

	return true, chars, startIndex
}

// / <summary>Checks for UTF-16 sequence, BE only (i.e. ASCII - 00 pairs).  Expect a 00 00 terminator.</summary>
// / <param name="src">bytes to find string in</param>
// / <param name="startIndex">current starting point; this is updated on return.</param>
// / <param name="minLen">String must be this long to qualify</param>
// / <returns>Tuple of whether a string was found, and if so, what.</returns>
func GetUTF16String(src []byte, index int, minLen int) (bool, string, int) {
	foundString := ""
	success := true
	for success {
		if index+2 > len(src) { // EOF
			success = false
			break
		}
		if (isASCII(src[index])) && (src[index+1] == 0) { // Valid ASCII
			foundString += string(src[index])
			index += 2
			continue
		} else if (len(foundString) > 1) && (src[index] == 0) && (src[index+1] == 0) { // Null terminator
			foundString = strings.TrimSpace(foundString)
			break
		}
		success = false // Failure.
	}
	if (success) && (len(foundString) >= minLen) { // Got the null terminator
		index = index + 2
	} else {
		success = false // Not successful if too short.
	}

	return success, foundString, index // Some stuff, perhaps PDFs, fall through here with a ton of short codes and \n.
}

// If we are in dated-file-avoidance-mode, is this file new?
// skipString is the <param name="fname">File name we're checking</param>
// <param name="skipString">Where the date or incrementor starts</param>
// <returns>True if this is the first of its kind.</returns>
func PassesFileMatch(fname string, skipString string) bool {
	if len(skipString) == 0 {
		return true
	}
	skipIndex := strings.Index(fname, skipString)

	checkName := IIF(skipIndex >= 0, fname[:skipIndex], fname)
	if slices.Contains(baseNames, checkName) {
		return false
	}
	baseNames = append(baseNames, checkName)
	return true
}