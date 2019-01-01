# perl
# livedl.exe内のローカルパスの文字列を隠す
use strict;
use v5.20;

for my $file("livedl2.exe") {
	open my $f, "<:raw", $file or die;
	undef $/;
	my $s = <$f>;
	close $f;

	say "$0: $file";

	my %h = ();

	while($s =~ m{(?<=\0)[^\0]{5,512}\.go(?=\0)|(?<=[[:cntrl:]])_/[A-Z]_/[^\0]{5,512}}g) {
		my $s = $&;
		if($s =~ m{\A(.*(?:/Users/.+?/go/src|/Go/src))(/.*)\z}s or
		$s =~ m{\A(.*(?=/livedl[^/]*/src/))(/.*)\z}s) {
			my($all, $p, $f) = ($s, $1, $2);

			my $p2 = $p;
			$p2 =~ s{.}{*}gs;
			#$h{$all} = $p2 . $f;

			#say $p;
			$h{$p} = $p2;
		}
	}

	for my $k (sort{$a cmp $b} keys %h) {
		my $k2 = $k;
		$k2 =~ s{/}{\\}g;

		my $r = quotemeta $k;
		my $r2 = quotemeta $k2;

		say "$k => $h{$k}";

		$s =~ s{$r}{$h{$k}}g;
		$s =~ s{$r2}{$h{$k}}g;
	}

	open $f, ">:raw", $file or die;
	print $f $s;
	close $f;

	sleep 1;
}
