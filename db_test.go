package db

import "testing"

func TestDbFilePath(t *testing.T) {
	type args struct {
		name     string
		dbFolder string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "with db folder",
			args: args{
				name:     "test",
				dbFolder: "db",
			},
			want: "db/test.db",
		},
		{
			name: "without db folder",
			args: args{
				name:     "test",
				dbFolder: "",
			},
			want: "test.db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := DbFilePath(tt.args.name, tt.args.dbFolder)
			if got != tt.want {
				t.Errorf("DbFilePath() got = %v, want %v", got, tt.want)
			}
		})
	}
}
