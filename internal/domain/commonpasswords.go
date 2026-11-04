package domain

import "strings"

// commonPasswords is a small list of very common passwords that are at least 10
// characters long (shorter ones fail the length rule anyway). Compared in lower case (BR-20).
var commonPasswords = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, p := range strings.Fields(`
		1234567890 0123456789 9876543210 12345678910 123456789012 1234567890123 0987654321
		1111111111 0000000000 2222222222 5555555555 9999999999 1212121212 1231231234
		1q2w3e4r5t 1q2w3e4r5t6y 1qaz2wsx3edc 1qaz@wsx3edc q1w2e3r4t5 q1w2e3r4t5y6
		qwertyuiop qwertyuiop1 qwertyuiop123 qwerty1234 qwerty12345 qwerty123456
		qwertyuiopasdfghjkl asdfghjkl asdfghjkl1 asdfghjkl123 zxcvbnm123 zxcvbnmasdf
		qazwsxedcrfv qazwsxedc123 zaq12wsxcde3 abcdefghij abcd123456 abcdef1234 abc1234567
		password12 password123 password1234 password12345 password1! password123! passw0rd123
		password01 password11 password99 passwordpassword mypassword1 mypassword123 mypassword12
		letmein123 letmein1234 welcome123 welcome1234 welcome12345 changeme123 changeme12 changemenow
		administrator admin12345 admin123456 admin1234 admin123! adminadmin adminadmin1 admin@12345
		administrator1 administrator123 root123456 rootroot12 superuser1 supervisor1
		iloveyou12 iloveyou123 iloveyou1234 sunshine123 princess123 football123 baseball123
		monkey1234 dragon1234 master1234 shadow1234 trustno1234 trustno1ab superman123 batman1234
		starwars123 whatever123 freedom123 computer123 internet123 michael123 jessica123
		hotspot123 hotspot1234 hotspotplus hotspotplus1 hotspotplus123 wifipassword wifi123456 mikrotik123
		mikrotik1234 routeros123 router12345 default1234 default123! secret1234 secret12345 test123456
		testtest12 testing1234 guest12345 login12345 system1234 pass123456 pass1234567
		p@ssw0rd123 p@ssword12 p@ssw0rd12! p@$$w0rd12 p4ssw0rd12
	`) {
		m[p] = struct{}{}
	}
	return m
}()
